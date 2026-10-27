package contract_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"github.com/jackc/pgx/v5/pgxpool"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/server"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGroupRoutingUXHTTPCollectionKeepsOriginalRouteAfterGroupChange(t *testing.T) {
	runChannel168CollectionKeepsOriginalRouteAfterGroupChange(t, nil)
}
func TestChannel168CollectionKeepsOriginalRouteAfterGroupChange(t *testing.T) {
	runChannel168CollectionKeepsOriginalRouteAfterGroupChange(t, installChannel168Fees)
}
func runChannel168CollectionKeepsOriginalRouteAfterGroupChange(t *testing.T, channelFees channel168FixturePolicy) {
	s, pool, _, calls := routingHTTPFixture(t)
	ctx := context.Background()
	if channelFees != nil {
		applyChannel168Fixture(t, s, channelFees)
	}
	routingPolicy(t, s, "1", "first", "7:1", "{}", "{}")
	var gid int64
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"8","type":"channel"}}}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=1`, gid); e != nil {
		t.Fatal(e)
	}
	var id string
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('collection-codes',1,'{"title":"group collection","name":"local","enabled":true}') RETURNING id::text`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/merchant/collection-codes/"+id+"/link", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var link struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &link) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	post := func(no string) {
		t.Helper()
		r := httptest.NewRequest("POST", link.Data.URL, strings.NewReader(url.Values{"amount": {"1.00"}, "type": {"alipay"}, "external_no": {no}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 302 {
			t.Fatal("group collection", w.Code, w.Body.String())
		}
	}
	post("group152-original-intent")
	post("group152-original-intent")
	trade := routingTrade(t, s, 1, "collect:"+id+":group152-original-intent")
	if sent := calls.list(trade); len(sent) != 1 || sent[0] != 8 {
		t.Fatal("collection ignored group", sent)
	}
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info,alipay,channel}','"7"') WHERE id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	post("group152-original-intent")
	if sent := calls.list(trade); len(sent) != 1 || sent[0] != 8 {
		t.Fatal("collection original changed", sent)
	}
	post("group152-new-intent")
	if sent := calls.list(routingTrade(t, s, 1, "collect:"+id+":group152-new-intent")); len(sent) != 1 || sent[0] != 7 {
		t.Fatal("new collection ignored changed group", sent)
	}
	bal, e := s.Store.Balance(ctx, 1)
	if e != nil || bal.Available != 0 || bal.Reserved != 0 {
		t.Fatal("unverified collection credited funds", bal, e)
	}
}

func TestGroupRoutingUXHTTPRechargeRetainsOriginalMaterialAndRejectsExplicitBypass(t *testing.T) {
	runChannel168RechargeRetainsOriginalMaterialAndRejectsExplicitBypass(t, nil)
}
func TestChannel168RechargeRetainsOriginalMaterialAndRejectsExplicitBypass(t *testing.T) {
	runChannel168RechargeRetainsOriginalMaterialAndRejectsExplicitBypass(t, installChannel168Fees)
}
func runChannel168RechargeRetainsOriginalMaterialAndRejectsExplicitBypass(t *testing.T, channelFees channel168FixturePolicy) {
	s, pool, calls := groupRechargeHTTPFixture152(t)
	ctx := context.Background()
	if channelFees != nil {
		applyChannel168Fixture(t, s, channelFees)
	}
	rule := routingPolicy(t, s, "1", "first", "7:1", "{}", "{}")
	routingPatch(t, s, rule, map[string]any{"type": "wxpay"})
	var gid int64
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"wxpay":{"channel":"8","type":"channel"}}}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=1`, gid); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	post := func(no, extra string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "/console/v1/merchant/workflows/recharges", strings.NewReader(`{"amount":"1.00","type":"wxpay","method":"native","external_no":"`+no+`"`+extra+`}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	for i := 0; i < 2; i++ {
		if w := post("group152-original-intent", ""); w.Code != 201 {
			t.Fatal("group recharge", w.Code, w.Body.String())
		}
	}
	trade := routingTrade(t, s, 1, "recharge:group152-original-intent")
	if sent := calls.list(trade); len(sent) != 1 || sent[0] != 8 {
		t.Fatal("recharge ignored group", sent)
	}
	if w := post("explicit-bypass", `,"channel_id":"7"`); w.Code != 422 {
		t.Fatal("recharge explicit bypass", w.Code, w.Body.String())
	}
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info,wxpay,channel}','"7"') WHERE id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	if w := post("group152-original-intent", ""); w.Code != 201 {
		t.Fatal("original recharge lost after group change", w.Code, w.Body.String())
	}
	if sent := calls.list(trade); len(sent) != 1 || sent[0] != 8 {
		t.Fatal("original recharge resent", sent)
	}
	if w := post("group152-new-intent", ""); w.Code != 201 {
		t.Fatal("new recharge", w.Code, w.Body.String())
	}
	if sent := calls.list(routingTrade(t, s, 1, "recharge:group152-new-intent")); len(sent) != 1 || sent[0] != 7 {
		t.Fatal("new recharge ignored changed group", sent)
	}
	var orders, attempts int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts)`).Scan(&orders, &attempts); e != nil || orders != 2 || attempts != 2 {
		t.Fatal("rejected or repeated recharge left extra intent", orders, attempts, e)
	}
	bal, e := s.Store.Balance(ctx, 1)
	if e != nil || bal.Available != 0 || bal.Reserved != 0 {
		t.Fatal("unverified recharge credited funds", bal, e)
	}
}

func groupRechargeHTTPFixture152(t *testing.T) (*server.Server, *pgxpool.Pool, *routingCalls) {
	s, pool, _, calls := routingHTTPFixture(t)
	const apiKey = "0123456789abcdef0123456789abcdef"
	var unknown atomic.Bool
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pay/unifiedorder" {
			t.Error("unexpected recharge payment product", r.URL.Path)
			http.Error(w, "unexpected", 500)
			return
		}
		var request struct {
			AppID  string `xml:"appid"`
			Trade  string `xml:"out_trade_no"`
			Kind   string `xml:"trade_type"`
			Amount string `xml:"total_fee"`
		}
		if e := xml.NewDecoder(r.Body).Decode(&request); e != nil {
			t.Error(e)
			return
		}
		if request.Kind != "NATIVE" || request.Amount != "100" {
			t.Error("wrong recharge request", request)
		}
		channel := int64(7)
		if request.AppID == "wx-eight" {
			channel = 8
		} else if request.AppID != "wx-seven" {
			t.Error("unknown recharge application", request.AppID)
		}
		calls.mu.Lock()
		calls.channels[request.Trade] = append(calls.channels[request.Trade], channel)
		calls.mu.Unlock()
		if unknown.Load() {
			w.Write([]byte(`<xml><return_code>SUCCESS</return_code></xml>`))
			return
		}
		response := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": request.AppID, "mch_id": "mch", "trade_type": "NATIVE", "code_url": "weixin://wxpay/synthetic/" + request.Trade}
		response["sign"], _ = adapter.SignWechatV2(response, apiKey, "MD5")
		w.Write(providerXML(response))
	}))
	t.Cleanup(up.Close)
	for id, app := range map[int64]string{7: "wx-seven", 8: "wx-eight"} {
		p, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: app, MerchantID: "mch", APIKey: apiKey, Methods: []string{"native"}, BaseURL: up.URL, HTTPClient: up.Client()})
		if e != nil {
			t.Fatal(e)
		}
		s.ProviderFlow.Providers[id] = p
	}
	return s, pool, calls
}
