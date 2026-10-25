package contract_test

import (
	"context"
	"encoding/xml"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPayRollRechargeUsesStickyRoundRobinAndRejectsQuotaBeforeOrder(t *testing.T) {
	s, pool, _, calls := routingHTTPFixture(t)
	ctx := context.Background()
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
	defer up.Close()
	for id, app := range map[int64]string{7: "wx-seven", 8: "wx-eight"} {
		p, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: app, MerchantID: "mch", APIKey: apiKey, Methods: []string{"native"}, BaseURL: up.URL, HTTPClient: up.Client()})
		if e != nil {
			t.Fatal(e)
		}
		s.ProviderFlow.Providers[id] = p
	}
	rule := routingPolicy(t, s, "1", "round_robin", "7:1,8:1", "{}", "{}")
	routingPatch(t, s, rule, map[string]any{"type": "wxpay", "limits": `{"daily_count":2}`})
	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	post := func(no, extra string) *httptest.ResponseRecorder {
		t.Helper()
		body := `{"amount":"1.00","type":"wxpay","method":"native","external_no":"` + no + `"` + extra + `}`
		r := httptest.NewRequest("POST", "/console/v1/merchant/workflows/recharges", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	for i, want := range []int64{7, 8} {
		no := []string{"recharge-one", "recharge-two"}[i]
		for j := 0; j < 2; j++ {
			w := post(no, "")
			if w.Code != 201 {
				t.Fatal("recharge creation/retry failed", w.Code, w.Body.String())
			}
		}
		trade := routingTrade(t, s, 1, "recharge:"+no)
		if sent := calls.list(trade); len(sent) != 1 || sent[0] != want {
			t.Fatal("recharge bypassed sticky policy", sent, want)
		}
	}
	if w := post("recharge-over-quota", ""); w.Code != 422 {
		t.Fatal("recharge quota rejected after order creation", w.Code, w.Body.String())
	}
	var assigned, orders, intents, attempts int
	if e = pool.QueryRow(ctx, "SELECT (SELECT assigned FROM provider_route_counters WHERE rule_id=$1),(SELECT count(*) FROM orders),(SELECT count(*) FROM recharge_intents),(SELECT count(*) FROM provider_attempts)", rule).Scan(&assigned, &orders, &intents, &attempts); e != nil || assigned != 2 || orders != 2 || intents != 2 || attempts != 2 {
		t.Fatal("recharge quota/retry leaked data", assigned, orders, intents, attempts, e)
	}
	routingPatch(t, s, rule, map[string]any{"limits": "{}"})
	unknown.Store(true)
	for i := 0; i < 2; i++ {
		if w := post("recharge-unknown", ""); w.Code != 202 {
			t.Fatal("unknown recharge outcome not preserved", w.Code, w.Body.String())
		}
	}
	trade := routingTrade(t, s, 1, "recharge:recharge-unknown")
	if sent := calls.list(trade); len(sent) != 1 || sent[0] != 7 {
		t.Fatal("unknown recharge silently failed over", sent)
	}
	if w := post("recharge-unknown", `,"channel_id":"8"`); w.Code != 422 {
		t.Fatal("changed channel reused unknown recharge", w.Code, w.Body.String())
	}
	bal, e := s.Store.Balance(ctx, 1)
	if e != nil || bal.Available != 0 {
		t.Fatal("recharge endpoint credited unverified funds", bal, e)
	}
}
