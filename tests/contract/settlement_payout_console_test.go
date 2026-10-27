package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type settlementHTTPProvider struct {
	adapter.Provider
	writes, reads atomic.Int32
}

func (*settlementHTTPProvider) ApplicationID() string   { return "settlement-http-app" }
func (*settlementHTTPProvider) TransferTypes() []string { return []string{"alipay"} }
func (p *settlementHTTPProvider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.writes.Add(1)
	return adapter.TransferResult{}, &adapter.UnknownOutcome{Operation: "settlement lost response"}
}
func (p *settlementHTTPProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.reads.Add(1)
	return adapter.TransferResult{TransferNo: r.TransferNo, Amount: r.Amount, State: "SUCCESS", ProviderTransferNo: "settlement-provider-1"}, nil
}

func TestSettlementPayoutConsoleScopedReadPrepareUnknownAndOriginalQuery(t *testing.T) {
	s, p, key := fixture(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"3"}' WHERE id=1; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}'),('settings','{"key":"settlement_fee_rate_bps","value":"250"}')`); err != nil {
		t.Fatal(err)
	}
	o, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "settlement-console-source", Amount: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.MarkPaid(ctx, o.TradeNo, "settlement-console-source"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "alipay", Account: "private-settlement-account", Name: "Private Settlement Name"}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant"}); err != nil {
		t.Fatal(err)
	}
	v, err := s.Store.RequestFunds(ctx, "settlements", 1, 0, "settlement-console", 1000, "merchant:1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.ReviewFundsRequest(ctx, v.ID, true, "admin:1"); err != nil {
		t.Fatal(err)
	}
	ali, err := adapter.NewAlipay(adapter.AlipayConfig{AppID: "settlement-http-app", PrivateKey: key, PublicKey: &key.PublicKey})
	if err != nil {
		t.Fatal(err)
	}
	provider := &settlementHTTPProvider{Provider: ali}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: provider}, AllowLive: true}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	adminToken, adminCSRF, err := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AdminClass: "super"})
	if err != nil {
		t.Fatal(err)
	}
	merchantToken, merchantCSRF, err := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(role, method, suffix, body, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, fmt.Sprintf("/console/v1/%s/settlement-payouts/%d%s", role, v.ID, suffix), strings.NewReader(body))
		token := adminToken
		if role == "merchant" {
			token = merchantToken
		}
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := invoke("merchant", "GET", "", "", "")
	if w.Code != 200 {
		t.Fatal("settlement detail not available", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private-settlement-account") || strings.Contains(w.Body.String(), "Private Settlement Name") {
		t.Fatal("recipient plaintext in detail", w.Body.String())
	}
	if w = invoke("merchant", "POST", "/prepare", `{"channel_id":"7"}`, merchantCSRF); w.Code != 403 {
		t.Fatal("merchant prepared payout", w.Code, w.Body.String())
	}
	if w = invoke("admin", "POST", "/prepare", `{"channel_id":"7"}`, ""); w.Code != 403 {
		t.Fatal("payout ignored csrf", w.Code)
	}
	if w = invoke("admin", "GET", "/capabilities", "", ""); w.Code != 200 {
		t.Fatal("payout capabilities", w.Code, w.Body.String())
	}
	w = invoke("admin", "POST", "/prepare", `{"channel_id":"7"}`, adminCSRF)
	if w.Code != 200 {
		t.Fatal("prepare debited available again", w.Code, w.Body.String())
	}
	if provider.writes.Load() != 0 {
		t.Fatal("prepare sent real request")
	}
	readAdmin := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/admin"+path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: adminToken})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	checkSettlementStats := func(state, key string) {
		t.Helper()
		w := readAdmin("/workflows/settlements/statistics?dstatus=" + state)
		var v struct{ Data map[string]string }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Data["total_count"] != "1" || v.Data[key] != "1" || v.Data["total_money"] != "10.00" {
			t.Fatal("original payout statistics", w.Code, w.Body.String())
		}
	}
	checkSettlementStats("0", "waiting_count")
	listed := readAdmin("/transfers?merchant_id=1")
	var list struct {
		Data struct{ Items []map[string]any }
	}
	if err = json.Unmarshal(listed.Body.Bytes(), &list); err != nil || listed.Code != 200 || len(list.Data.Items) != 1 {
		t.Fatal("payout list", listed.Code, listed.Body.String(), err)
	}
	row := list.Data.Items[0]
	if row["status"] != "unsent" || row["funding_source"] != "settlement" || row["settlement_request_id"] != fmt.Sprint(v.ID) {
		t.Fatal("payout list lost funding source or send state", row)
	}
	w = invoke("admin", "POST", "/execute", `{}`, adminCSRF)
	if w.Code != 202 {
		t.Fatal("unknown was hidden", w.Code, w.Body.String())
	}
	if provider.writes.Load() != 1 {
		t.Fatal("wrong outgoing count", provider.writes.Load())
	}
	if w = invoke("admin", "POST", "/execute", `{}`, adminCSRF); w.Code >= 500 {
		t.Fatal("original execute could not be read", w.Code, w.Body.String())
	}
	if provider.writes.Load() != 1 {
		t.Fatal("unknown was resent")
	}
	checkSettlementStats("2", "processing_count")
	s.ProviderFlow.AllowLive = false
	w = invoke("merchant", "POST", "/query", `{}`, merchantCSRF)
	if w.Code != 200 {
		t.Fatal("stopped original query", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			State string `json:"state"`
		}
	}
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Data.State != "succeeded" {
		t.Fatal("query not verified success", w.Body.String(), err)
	}
	b, err := s.Store.Balance(ctx, 1)
	if err != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatal("funding not settled once", b, err)
	}
	var fee int64
	if err = p.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees'").Scan(&fee); err != nil || fee != 25 {
		t.Fatal("settlement fee doubled or absent", fee, err)
	}
	checkSettlementStats("1", "completed_count")
	applications := readAdmin("/workflows/settlements")
	var applicationList struct {
		Data struct {
			Items []struct {
				Payout *payment.SettlementPayout `json:"payout"`
			}
		}
	}
	if err = json.Unmarshal(applications.Body.Bytes(), &applicationList); err != nil || len(applicationList.Data.Items) != 1 || applicationList.Data.Items[0].Payout == nil || applicationList.Data.Items[0].Payout.State != "succeeded" {
		t.Fatal("application list hid the original verified payout", applications.Code, applications.Body.String(), err)
	}
	for _, kind := range []string{"transfer-accounts", "transfer-channels", "transfer-merchants"} {
		report := readAdmin("/reports/" + kind)
		var out struct {
			Data struct{ Items []map[string]string }
		}
		if err = json.Unmarshal(report.Body.Bytes(), &out); err != nil || report.Code != 200 || len(out.Data.Items) != 1 {
			t.Fatal("payout report", kind, report.Code, report.Body.String(), err)
		}
		item := out.Data.Items[0]
		if item["paid_money"] != "9.75" || item["net_money"] != "10.00" || item["fee_money"] != "0.25" {
			t.Fatal("payout report discarded gross settlement debit or charged wrong fee", kind, item)
		}
		csv := readAdmin("/reports/" + kind + "?format=csv")
		if csv.Code != 200 || !strings.Contains(csv.Body.String(), "9.75,10.00,0.25") {
			t.Fatal("payout report csv", kind, csv.Code, csv.Body.String())
		}
	}
}
