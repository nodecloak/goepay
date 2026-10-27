package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type subchannel168BFactory struct {
	mu        sync.Mutex
	providers map[string]*channel168Provider
	schema    string
}

func (f *subchannel168BFactory) Schema() adapter.SubchannelSchema {
	return adapter.SubchannelSchema{ParentID: 7, Revision: 1, PayType: "alipay", Fingerprint: f.schemaFingerprint(), Fields: []adapter.SubchannelField{{Name: "app", Required: true}, {Name: "secret", Secret: true, Required: true}}}
}
func (f *subchannel168BFactory) Build(v map[string]string) (adapter.SubchannelExecution, error) {
	if len(v) != 2 || v["app"] == "" || v["secret"] != "synthetic-only-secret" {
		return adapter.SubchannelExecution{}, adapter.ErrInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.providers[v["app"]]
	if p == nil {
		p = &channel168Provider{app: v["app"]}
		f.providers[v["app"]] = p
	}
	return adapter.SubchannelExecution{Payment: p, SchemaFingerprint: f.Schema().Fingerprint}, nil
}
func subchannel168BLocal(t *testing.T) (channel168Fixture, *subchannel168BFactory) {
	t.Helper()
	f := channel168Local(t)
	factory := &subchannel168BFactory{providers: map[string]*channel168Provider{}}
	f.s.ProviderFlow.SubchannelFactories = map[int64]adapter.SubchannelFactory{7: factory}
	if _, e := f.p.Exec(context.Background(), `UPDATE backoffice_records SET body=body||'{"info":{"alipay":{"channel":"-2","type":"channel"}}}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	return f, factory
}
func (f channel168Fixture) childHTTP(method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/payment-subchannels"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func TestSubchannel168BActualHTTPRoutesAndSendsUnderOriginalChildAccount(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Synthetic child","enabled":true,"weight":1,"min_amount":"1","max_amount":"10000","values":{"app":"child-v1","secret":"synthetic-only-secret"}}`)
	if w.Code != 200 {
		t.Fatalf("create=%d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "synthetic-only-secret") || strings.Contains(w.Body.String(), "child-v1") {
		t.Fatal("private child values escaped DTO")
	}
	var data struct {
		Data struct {
			ID string `json:"id"`
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &data) != nil || data.Data.ID == "" {
		t.Fatal("child ID absent")
	}
	result := f.pay(t, "child168b-first", "1.00", 7)
	if result["code"] != float64(1) {
		t.Fatal("-2 did not execute its owned child", result)
	}
	var parent, child int64
	if e := f.p.QueryRow(context.Background(), `SELECT a.channel_id,c.subchannel_id FROM provider_attempts a JOIN orders o ON o.id=a.order_id JOIN payment_subchannel_routes c ON c.merchant_id=o.merchant_id AND c.out_trade_no=o.out_trade_no WHERE o.out_trade_no='child168b-first'`).Scan(&parent, &child); e != nil || parent != 7 || child <= 0 {
		t.Fatal("real parent/child namespace not bound", parent, child, e)
	}
	if factory.providers["child-v1"].sends.Load() != 1 || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
		t.Fatal("first send borrowed parent execution")
	}
	again := f.pay(t, "child168b-first", "1.00", 7)
	if again["code"] != float64(1) || factory.providers["child-v1"].sends.Load() != 1 {
		t.Fatal("replay sent another payment", again)
	}
}
func (f *subchannel168BFactory) Snapshot(v map[string]string) ([]byte, error) {
	if _, e := f.Build(v); e != nil {
		return nil, e
	}
	return json.Marshal(struct {
		Values map[string]string
		Schema string
	}{v, f.Schema().Fingerprint})
}
func (f *subchannel168BFactory) Restore(b []byte) (adapter.SubchannelExecution, error) {
	var frozen struct {
		Values map[string]string
		Schema string
	}
	if json.Unmarshal(b, &frozen) != nil {
		return adapter.SubchannelExecution{}, adapter.ErrInvalid
	}
	out, e := f.Build(frozen.Values)
	out.SchemaFingerprint = frozen.Schema
	return out, e
}

func TestSubchannel168BActualHTTPAccountRevisionNeverReinterpretsOriginalAttempt(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Synthetic child","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"child-v1","secret":"synthetic-only-secret"}}`)
	if w.Code != 200 {
		t.Fatalf("create=%d %s", w.Code, w.Body.String())
	}
	var state struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &state) != nil {
		t.Fatal("bad create envelope")
	}
	if out := f.pay(t, "child168b-before-edit", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": state.Data["fingerprint"], "values": map[string]any{"app": map[string]string{"operation": "replace", "value": "child-v2"}, "secret": map[string]string{"operation": "keep"}}})
	w = f.childHTTP("PATCH", "/"+state.Data["id"].(string), string(body))
	if w.Code != 200 {
		t.Fatalf("immutable account replacement unavailable: status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "child-v2") || strings.Contains(w.Body.String(), "synthetic-only-secret") {
		t.Fatal("replacement leaked private material")
	}
	if out := f.pay(t, "child168b-before-edit", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("original route could not restore v1", out)
	}
	if out := f.pay(t, "child168b-after-edit", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("new route could not use v2", out)
	}
	if factory.providers["child-v1"].sends.Load() != 1 || factory.providers["child-v2"].sends.Load() != 1 {
		t.Fatal("account revision changed or resent original attempt")
	}
	var versions int
	if e := f.p.QueryRow(context.Background(), `SELECT count(*) FROM payment_subchannel_accounts`).Scan(&versions); e != nil || versions != 2 {
		t.Fatal("original account version was not preserved", versions, e)
	}
}

func TestSubchannel168BActualHTTPMerchantRollPolicyIsExecutableAndCASBound(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	w := f.childHTTP("GET", "/policy?merchant_id=1", "")
	if w.Code != 200 {
		t.Fatalf("child selection policy unavailable: %d %s", w.Code, w.Body.String())
	}
	var v struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal("policy envelope")
	}
	body, _ := json.Marshal(map[string]any{"roll_type": 1, "expected_fingerprint": v.Data["fingerprint"]})
	w = f.childHTTP("PATCH", "/policy?merchant_id=1", string(body))
	if w.Code != 200 {
		t.Fatalf("policy update=%d %s", w.Code, w.Body.String())
	}
	w = f.childHTTP("PATCH", "/policy?merchant_id=1", string(body))
	if w.Code != 409 {
		t.Fatalf("stale policy CAS reused: %d", w.Code)
	}
}

func TestSubchannel168BActualHTTPListUsesExactIDScopeAndBoundedPages(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	if _, e := f.p.Exec(context.Background(), `SELECT setval(pg_get_serial_sequence('payment_subchannels','id'),9007199254740993,false)`); e != nil {
		t.Fatal(e)
	}
	w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Exact large child","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"large-child","secret":"synthetic-only-secret"}}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.childHTTP("GET", "?id=9007199254740993&page=1&size=10&type=alipay", "")
	if w.Code != 200 {
		t.Fatalf("actual filtered list=%d %s", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			Items []map[string]any
			Total int
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.Total != 1 || len(out.Data.Items) != 1 || out.Data.Items[0]["id"] != "9007199254740993" {
		t.Fatal("exact child filter or ID precision lost", w.Body.String())
	}
	w = f.childHTTP("GET", "?id=9007199254740993&page=2&size=10&type=alipay", "")
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.Total != 1 || len(out.Data.Items) != 0 {
		t.Fatal("count/page predicate differs", w.Code, w.Body.String())
	}
}

func TestSubchannel168BActualHTTPConfirmedBrowserTestKeepsOneOriginalIntent(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Synthetic child","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"test-child","secret":"synthetic-only-secret"}}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var state struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &state)
	path := "/" + state.Data["id"].(string) + "/test"
	body := `{"amount":"1.00","intent":"0123456789abcdef0123456789abcdef"}`
	for i := 0; i < 2; i++ {
		w = f.childHTTP("POST", path, body)
		if w.Code != 200 {
			t.Fatalf("protected original test intent unavailable: %d %s", w.Code, w.Body.String())
		}
		var result struct{ Data map[string]any }
		json.Unmarshal(w.Body.Bytes(), &result)
		if result.Data["state"] != "pending" || result.Data["url"] == "" || result.Data["trade_no"] == "" {
			t.Fatal("test did not return original protected payment context", w.Body.String())
		}
	}
	if factory.providers["test-child"].sends.Load() != 1 {
		t.Fatal("testing resubmitted the same external payment intent")
	}
}

func TestSubchannel168BActualHTTPProjectsOnlyVerifiedFieldPresenceAndActualProducts(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Synthetic child","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"test-child","secret":"synthetic-only-secret"}}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.childHTTP("GET", "", "")
	var out struct {
		Data struct{ Items []map[string]any }
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Data.Items) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	row := out.Data.Items[0]
	if row["browser_test_available"] != true || row["account_available"] != true {
		t.Fatal("typed list has no original execution projection", row)
	}
	fields, ok := row["fields"].([]any)
	if !ok || len(fields) != 2 {
		t.Fatal("field presence absent", row)
	}
	for _, field := range fields {
		v := field.(map[string]any)
		if v["present"] != true || v["value"] != nil {
			t.Fatal("field state does not preserve private input", v)
		}
	}
	if strings.Contains(w.Body.String(), "test-child") || strings.Contains(w.Body.String(), "synthetic-only-secret") {
		t.Fatal("list leaked adapter credentials")
	}
}

func TestSubchannel168BExpiryAfterChildHeadWaitRollsBackEveryAssignment(t *testing.T) {
	subchannel168BExpiryAcrossWrite(t, "payment_subchannel_last_used", true)
}
func TestSubchannel168BExpiryAfterChildQuoteWaitNeedsFinalGuard(t *testing.T) {
	subchannel168BExpiryAcrossWrite(t, "provider_route_fee_quotes", false)
}
func subchannel168BExpiryAcrossWrite(t *testing.T, target string, allowConflict bool) {
	f, factory := subchannel168BLocal(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Synthetic child","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"expiry-child","secret":"synthetic-only-secret"}}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var vip int64
	if e := f.p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"-2","type":"channel"}}}') RETURNING id`).Scan(&vip); e != nil {
		t.Fatal(e)
	}
	trigger := fmt.Sprintf(`CREATE FUNCTION subchannel168b_expiry_wait() RETURNS trigger LANGUAGE plpgsql AS $$DECLARE selected_group text;BEGIN SELECT rule_snapshot->>'group_id' INTO selected_group FROM provider_route_assignments WHERE merchant_id=NEW.merchant_id AND out_trade_no=NEW.out_trade_no;IF selected_group IS DISTINCT FROM '%d' THEN RAISE EXCEPTION 'test did not acquire original VIP route';END IF;PERFORM pg_sleep(1.2);RETURN NEW;END$$;CREATE TRIGGER subchannel168b_expiry_wait BEFORE INSERT OR UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION subchannel168b_expiry_wait();`, vip, target)
	if _, e := f.p.Exec(ctx, trigger); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(1,$1,clock_timestamp()+interval '0.75 seconds')`, vip); e != nil {
		t.Fatal(e)
	}
	_, _, e := f.s.providerRoutedRequest(ctx, 1, compat.Params{"type": "alipay", "method": "native", "name": "synthetic expiry", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "child168b-expiry"})
	if !errors.Is(e, payment.ErrState) && !(allowConflict && errors.Is(e, payment.ErrConflict)) {
		t.Fatal("late child head wait did not fail the membership guard", e)
	}
	var expired bool
	var route, source, quote, childHead, parentHead, attempt int
	if e = f.p.QueryRow(ctx, `SELECT (SELECT expires_at<clock_timestamp() FROM memberships WHERE merchant_id=1),(SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM payment_subchannel_routes),(SELECT count(*) FROM provider_route_fee_quotes),(SELECT count(*) FROM payment_subchannel_last_used),(SELECT count(*) FROM provider_channel_last_used),(SELECT count(*) FROM provider_attempts)`).Scan(&expired, &route, &source, &quote, &childHead, &parentHead, &attempt); e != nil || !expired || route != 0 || source != 0 || quote != 0 || childHead != 0 || parentHead != 0 || attempt != 0 {
		t.Fatal("late expiry retained child/parent/quote writes", expired, route, source, quote, childHead, parentHead, attempt, e)
	}
	if factory.providers["expiry-child"].sends.Load() != 0 {
		t.Fatal("expired route submitted externally")
	}
}
func (f *subchannel168BFactory) schemaFingerprint() string {
	if f.schema != "" {
		return f.schema
	}
	return strings.Repeat("a", 64)
}

func TestSubchannel168BActualHTTPCurrentFeeProjectionUsesSameParentAndGroupPolicy(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Synthetic child","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"fee-child","secret":"synthetic-only-secret"}}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	read := func(want float64, source string) {
		t.Helper()
		w := f.childHTTP("GET", "", "")
		var out struct {
			Data struct{ Items []map[string]any }
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Data.Items) != 1 {
			t.Fatal(w.Code, w.Body.String())
		}
		row := out.Data.Items[0]
		if row["fee_rate_bps"] != want || row["fee_source"] != source {
			t.Fatal("current fee projection cannot prove effective policy", row["fee_rate_bps"], row["fee_source"], want, source)
		}
	}
	read(0, "none")
	if _, e := f.p.Exec(context.Background(), `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	f.configure(t, 7, "99.50")
	read(50, "parent")
	if out := f.pay(t, "child-fee-original", "100.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	f.configure(t, 7, "90.00")
	read(1000, "parent")
	o, e := f.s.Store.Order(context.Background(), 1, "child-fee-original")
	if e != nil || o.PlatformFee != 50 || o.MerchantAmount != 9950 {
		t.Fatal("current display repriced historical child order", o.PlatformFee, o.MerchantAmount, e)
	}
	if _, e := f.p.Exec(context.Background(), `UPDATE backoffice_records SET body=jsonb_set(body,'{info,alipay,rate}','"99.00"') WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	read(100, "group")
}

func TestSubchannel168BActualHTTPTestLookupIsReadOnlyAndDoesNotResend(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Synthetic child","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"lookup-child","secret":"synthetic-only-secret"}}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var state struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &state)
	path := "/" + state.Data["id"].(string) + "/test"
	intent := "0123456789abcdef0123456789abcdef"
	if w = f.childHTTP("POST", path, `{"amount":"1.00","intent":"`+intent+`"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var original struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &original)
	var before int
	if e := f.p.QueryRow(context.Background(), `SELECT count(*) FROM backoffice_audit`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	w = f.childHTTP("GET", path+"?intent="+intent, "")
	if w.Code != 200 {
		t.Fatalf("original test lookup unavailable: %d %s", w.Code, w.Body.String())
	}
	var read struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &read)
	if read.Data["trade_no"] != original.Data["trade_no"] || read.Data["state"] != "pending" {
		t.Fatal("test lookup changed its original context", read)
	}
	var after int
	f.p.QueryRow(context.Background(), `SELECT count(*) FROM backoffice_audit`).Scan(&after)
	if before != after || factory.providers["lookup-child"].sends.Load() != 1 {
		t.Fatal("read-only test lookup mutated or resubmitted", before, after)
	}
}
func TestSubchannel168BActualHTTPCollectionLinkExecutesCanonicalChildAndLegacyAlias(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	ctx := context.Background()
	w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Synthetic child","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"collect-child","secret":"synthetic-only-secret"}}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var row struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &row)
	id := row.Data["id"].(string)
	var code int64
	if e := f.p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('collection-codes',1,'{"title":"Synthetic collection168b","enabled":true}') RETURNING id`).Scan(&code); e != nil {
		t.Fatal(e)
	}
	w = f.childHTTP("GET", "/"+id+"/collection-link", "")
	if w.Code != 200 {
		t.Fatalf("owned original collection link unavailable: %d %s", w.Code, w.Body.String())
	}
	var data struct {
		Data struct {
			URL string `json:"url"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &data)
	u, e := url.Parse(data.Data.URL)
	if e != nil || u.Query().Get("subchannel_id") != id || u.Query().Get("type") != "alipay" {
		t.Fatal("collection link did not bind canonical child", data)
	}
	for i, alias := range []bool{false, true} {
		q := u.Query()
		if alias {
			q.Del("subchannel_id")
			q.Set("channel_id", id)
		}
		u.RawQuery = q.Encode()
		get := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(get, httptest.NewRequest("GET", u.String(), nil))
		if get.Code != 200 {
			t.Fatal("child collection GET", get.Code, get.Body.String())
		}
		form := url.Values{"amount": {"1.00"}, "type": {"alipay"}, "external_no": {fmt.Sprintf("subchannel168b-collect-%d", i)}}
		request := httptest.NewRequest("POST", u.String(), strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		posted := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(posted, request)
		if posted.Code != 302 {
			t.Fatal("child collection payment failed", posted.Code, posted.Body.String())
		}
	}
	if factory.providers["collect-child"].sends.Load() != 2 || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
		t.Fatal("collection borrowed parent execution")
	}
	var childRoutes int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM payment_subchannel_routes WHERE subchannel_id=$1`, id).Scan(&childRoutes); e != nil || childRoutes != 2 {
		t.Fatal("collection did not persist exact child bindings", childRoutes, e)
	}
}

func TestSubchannel168BActualHTTPDeleteStopsNewRoutingButKeepsOriginalAccount(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Synthetic child","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"archive-child","secret":"synthetic-only-secret"}}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var row struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &row)
	id := row.Data["id"].(string)
	if out := f.pay(t, "archive168b-original", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": row.Data["fingerprint"]})
	w = f.childHTTP("DELETE", "/"+id, string(body))
	if w.Code != 200 {
		t.Fatalf("safe original deletion unavailable: %d %s", w.Code, w.Body.String())
	}
	w = f.childHTTP("GET", "", "")
	var page struct {
		Data struct {
			Items []map[string]any
			Total int
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Data.Total != 0 || len(page.Data.Items) != 0 {
		t.Fatal("deleted child remained in the live directory", w.Code, w.Body.String())
	}
	if out := f.pay(t, "archive168b-new", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("deleted child accepted a new payment")
	}
	o, e := f.s.Store.Order(context.Background(), 1, "archive168b-original")
	if e != nil {
		t.Fatal(e)
	}
	original, _, e := f.s.ProviderFlow.ForPaymentIntent(context.Background(), 1, o.OutTradeNo, 7)
	if e != nil || fmt.Sprint(original.SubchannelID()) != id || original.ValidatePaymentAccount(context.Background(), 1, o.TradeNo) != nil {
		t.Fatal("deleting changed the original account evidence", e)
	}
	var versions, routes int
	f.p.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM payment_subchannel_accounts),(SELECT count(*) FROM payment_subchannel_routes)`).Scan(&versions, &routes)
	if versions != 1 || routes != 1 || factory.providers["archive-child"].sends.Load() != 1 {
		t.Fatal("deleting erased source history or resent", versions, routes)
	}
}
