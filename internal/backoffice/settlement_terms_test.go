package backoffice

import (
	"context"
	"encoding/json"
	"goepay/internal/payment"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettlementPolicySettingsQuoteAndResponseSnapshots(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	admin := Principal{Role: "admin", SubjectID: 1, Username: "admin"}
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "merchant"}
	for key, value := range map[string]string{"settlement_fee_rate_bps": "250", "settlement_fee_minimum_cents": "3", "settlement_retained_cents": "10", "settlement_daily_limit": "3"} {
		raw, _ := json.Marshal(map[string]string{"key": key, "value": value})
		if _, e := s.Create(ctx, admin, "settings", input(string(raw))); e != nil {
			t.Fatal(key, e)
		}
	}
	for key, value := range map[string]string{"settlement_fee_rate_bps": "10001", "settlement_fee_maximum_cents": "-1", "settlement_daily_limit": "1000001"} {
		raw, _ := json.Marshal(map[string]string{"key": key, "value": value})
		if _, e := s.Create(ctx, admin, "settings", input(string(raw))); e == nil {
			t.Fatal("invalid setting accepted", key)
		}
	}
	o, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 101, OutTradeNo: "terms-http-funds", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.MarkPaid(ctx, o.TradeNo, "terms-http-funds"); e != nil {
		t.Fatal(e)
	}
	invoke := func(p Principal, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		s.ServeWorkflows(w, r, p, []string{strings.Split(strings.TrimPrefix(path, "/"), "?")[0]})
		return w
	}
	w := invoke(merchant, "GET", "/settlement-policy?amount=1.23", "")
	var out struct {
		Data struct {
			CanApply bool                     `json:"can_apply"`
			Terms    *payment.SettlementTerms `json:"terms"`
		} `json:"data"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil || w.Code != 200 || !out.Data.CanApply || out.Data.Terms == nil || out.Data.Terms.FeeAmount != 3 || out.Data.Terms.NetAmount != 120 {
		t.Fatalf("quote: %d %s %v", w.Code, w.Body.String(), e)
	}
	if w = invoke(merchant, "GET", "/settlement-policy?merchant_id=102&amount=1", ""); w.Code != 403 {
		t.Fatal("foreign quote", w.Code)
	}
	if w = invoke(admin, "GET", "/settlement-policy?amount=1", ""); w.Code != 422 {
		t.Fatal("admin without owner", w.Code)
	}
	w = invoke(merchant, "POST", "/settlements", `{"amount":"1.23","external_no":"terms-http","expected_fee_cents":"3","expected_net_cents":"120"}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"fee_amount":"3"`) {
		t.Fatal("create terms absent", w.Code, w.Body.String())
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"500"') WHERE resource='settings' AND body->>'key'='settlement_fee_rate_bps'`); e != nil {
		t.Fatal(e)
	}
	w = invoke(merchant, "POST", "/settlements", `{"amount":"1.23","external_no":"stale-quote","expected_fee_cents":"3","expected_net_cents":"120"}`)
	if w.Code != 409 {
		t.Fatal("stale client fee confirmation ignored", w.Code, w.Body.String())
	}
	w = invoke(merchant, "POST", "/settlements", `{"amount":"1.23","external_no":"terms-http","expected_fee_cents":"3","expected_net_cents":"120"}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"fee_amount":"3"`) {
		t.Fatal("historical quoted intent repriced", w.Code, w.Body.String())
	}
	w = invoke(merchant, "GET", "/settlements", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"net_amount":"120"`) {
		t.Fatal("list terms absent", w.Code, w.Body.String())
	}
}
