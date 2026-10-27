package server

import (
	"context"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/platform/jobs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRisk177ChildContinuousUnpaidRestrictsOnlyThatChildAndRetainsOriginalRecovery(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	risk := risk177Fixture{f.s, f.token, f.csrf}
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `SELECT setval(pg_get_serial_sequence('payment_subchannels','id'),9007199254741773,false)`); e != nil {
		t.Fatal(e)
	}
	first := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Synthetic risky child","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"risk177-child-one","secret":"synthetic-only-secret"}}`)
	if first.Code != 200 {
		t.Fatal("child create", first.Code)
	}
	var child struct{ Data map[string]any }
	if json.Unmarshal(first.Body.Bytes(), &child) != nil {
		t.Fatal("child json")
	}
	id := child.Data["id"].(string)
	v := risk177Data(t, risk.request("GET", "", risk.csrf))
	v = risk177Data(t, risk.request("PATCH", risk177Body(t, v, func(s map[string]any) {
		s["auto_check_channel"] = "1"
		s["check_channel_second"] = "3600"
		s["check_channel_failcount"] = "1"
		s["check_channel_notice"] = "1"
	}), risk.csrf))
	original := f.pay(t, "risk177-child-first", "1.00", 7)
	if original["code"] != float64(1) {
		t.Fatal(original)
	}
	risk.check(t, v, "risk177-child-original-check")
	actions := risk177Data(t, risk.riskPath("GET", "/actions", ""))
	items := actions["items"].([]any)
	if len(items) != 1 {
		t.Fatal("child check count", len(items))
	}
	a := items[0].(map[string]any)
	if a["kind"] != "subchannel" || a["target_id"] != id {
		t.Fatal("child failure closed parent instead", a["kind"], a["target_id"])
	}
	catalog := f.childHTTP("GET", "", "")
	if catalog.Code != 200 {
		t.Fatal("projection", catalog.Code)
	}
	var list struct {
		Data struct{ Items []map[string]any }
	}
	if json.Unmarshal(catalog.Body.Bytes(), &list) != nil {
		t.Fatal("catalog json")
	}
	if len(list.Data.Items) != 1 || list.Data.Items[0]["enabled"] != true || list.Data.Items[0]["browser_test_available"] != false || list.Data.Items[0]["core_risk"].(map[string]any)["blocked"] != true {
		t.Fatal("child effective state missing or changed manual enabled")
	}
	if target := os.Getenv("GOEPAY_RISK177_CHILD_UI_FIXTURE"); target != "" {
		expected := "e:/goepay/.tools/work-riskcheck177-39cdd806-b87f-4ff1-b6c1-f3d886c89eaa/.tools/integration184-v2/web/.tools/"
		absolute, e := filepath.Abs(target)
		if e != nil || !strings.HasPrefix(strings.ToLower(filepath.ToSlash(absolute)), expected) {
			t.Fatal("owned child fixture required")
		}
		read := func(w *httptest.ResponseRecorder) json.RawMessage {
			if w.Code != 200 {
				t.Fatal("fixture HTTP", w.Code)
			}
			var env struct{ Data json.RawMessage }
			if json.Unmarshal(w.Body.Bytes(), &env) != nil {
				t.Fatal("fixture shape")
			}
			return env.Data
		}
		raw, e := json.MarshalIndent(map[string]any{"admin": read(catalog), "schemas": read(f.childHTTP("GET", "/schemas", "")), "policy": read(f.childHTTP("GET", "/policy?merchant_id=1", ""))}, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(target, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if out := f.pay(t, "risk177-child-denied", "1.00", 7); out["code"] == float64(1) || factory.providers["risk177-child-one"].sends.Load() != 1 {
		t.Fatal("risk child sent new payment")
	}
	next := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Synthetic healthy child","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"risk177-child-two","secret":"synthetic-only-secret"}}`)
	if next.Code != 200 {
		t.Fatal("healthy child", next.Code)
	}
	if out := f.pay(t, "risk177-child-healthy", "1.00", 7); out["code"] != float64(1) || factory.providers["risk177-child-two"].sends.Load() != 1 {
		t.Fatal("blocked child prevented healthy child selection", out)
	}
	if out := f.pay(t, "risk177-child-first", "1.00", 7); out["code"] != float64(1) || factory.providers["risk177-child-one"].sends.Load() != 1 {
		t.Fatal("original replay was restricted or resent", out)
	}
	var queries atomic.Int32
	factory.providers["risk177-child-one"].subchannelTestQuery = func(_ context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
		queries.Add(1)
		return adapter.QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: "risk177-child-q-" + r.TradeNo, State: "SUCCESS", Currency: "CNY", Amount: 100}, nil
	}
	originalFlow, _, e := f.s.ProviderFlow.ForPaymentIntent(ctx, 1, "risk177-child-first", 7)
	if e != nil {
		t.Fatal("original account restore", e)
	}
	if _, e := originalFlow.Query(ctx, 1, original["trade_no"].(string)); e != nil {
		t.Fatal("original child query blocked", e)
	}
	if queries.Load() != 1 {
		t.Fatal("query did not reach the authenticated original child")
	}
	paid, e := f.s.Store.OrderByTradeNo(ctx, original["trade_no"].(string))
	if e != nil || paid.Status != "paid" {
		t.Fatal("original confirmed child query did not settle", e)
	}
	var notices int
	if e := f.p.QueryRow(ctx, `SELECT count(*)FROM core_risk_notices n JOIN core_risk_actions a ON a.id=n.action_id WHERE a.kind='subchannel'`).Scan(&notices); e != nil || notices != 1 {
		t.Fatal("child notice not in same original Check transaction", notices, e)
	}
	var delivered atomic.Int32
	sink := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if json.NewDecoder(r.Body).Decode(&body) != nil || !strings.Contains(body["title"], "风控") {
			t.Error("invalid frozen child notice")
		}
		delivered.Add(1)
		w.WriteHeader(204)
	}))
	defer sink.Close()
	resolver := risk177NoticeResolver{&risk177NoticeSender{client: sink.Client(), target: sink.URL}, "risk177-admin@example.invalid"}
	q := jobs.Queue{Pool: f.p}
	job, e := q.Claim(ctx, []string{"core-risk.notice"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.DeliverCoreRiskNotice(ctx, job.ID, job.Token, resolver); e != nil {
		t.Fatal("child admin notice consumer", e)
	}
	if e = f.s.Store.DeliverCoreRiskNotice(ctx, job.ID, job.Token, resolver); e != nil || delivered.Load() != 1 {
		t.Fatal("child notice replay resent", delivered.Load(), e)
	}
	current := f.childHTTP("GET", "/"+id, "")
	var live struct{ Data map[string]any }
	if current.Code != 200 || json.Unmarshal(current.Body.Bytes(), &live) != nil {
		t.Fatal("original child head read")
	}
	body, _ := json.Marshal(map[string]any{"enabled": false, "expected_fingerprint": live.Data["fingerprint"]})
	if w := f.childHTTP("PATCH", "/"+id, string(body)); w.Code != 200 {
		t.Fatal("manual child stop", w.Code)
	}
	release, _ := json.Marshal(map[string]any{"expected_fingerprint": a["fingerprint"], "expected_policy_fingerprint": v["fingerprint"]})
	risk177Data(t, risk.riskPath("POST", "/actions/"+a["id"].(string)+"/release", string(release)))
	current = f.childHTTP("GET", "/"+id, "")
	if current.Code != 200 || json.Unmarshal(current.Body.Bytes(), &live) != nil {
		t.Fatal("released child head read")
	}
	if live.Data["enabled"] != false || live.Data["core_risk"].(map[string]any)["blocked"] != false || live.Data["browser_test_available"] != false {
		t.Fatal("risk release reopened manually disabled child")
	}
}
