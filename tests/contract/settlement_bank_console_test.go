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
	"testing"
)

type settlementBankHTTPProvider struct {
	settlementHTTPProvider
	returned bool
}

func (*settlementBankHTTPProvider) TransferTypes() []string     { return []string{"bank"} }
func (*settlementBankHTTPProvider) BankTransferProduct() string { return adapter.BankProductAlipay }
func (p *settlementBankHTTPProvider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.writes.Add(1)
	return adapter.TransferResult{TransferNo: r.TransferNo, Amount: r.Amount, ProviderTransferNo: "local-settlement-bank", State: "PROCESSING", AlipayBank: &adapter.AlipayBankEvidence{Product: adapter.BankProductAlipay, SubmissionBound: true}}, nil
}
func (p *settlementBankHTTPProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.reads.Add(1)
	state := "SUCCESS"
	if p.returned {
		state = "REFUND"
	}
	return adapter.TransferResult{TransferNo: r.TransferNo, Amount: r.Amount, ProviderTransferNo: "local-settlement-bank", State: state, PayDate: "2026-10-23 12:00:00", AlipayBank: &adapter.AlipayBankEvidence{Product: adapter.BankProductAlipay, AmountKnown: true}}, nil
}

func TestSettlementBankConsoleCapabilitiesFeeHoldReportAndOriginalReturn(t *testing.T) {
	s, p, key := fixture(t)
	ctx := context.Background()
	if _, e := p.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true}' WHERE id=1; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}'),('settings','{"key":"settlement_fee_rate_bps","value":"250"}')`); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "bank-console-fund", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "bank-console-fund"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "bank", Account: "6222020000000000", Name: "Local Bank Recipient", BankCode: "1002"}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant"}); e != nil {
		t.Fatal(e)
	}
	v, e := s.Store.RequestFunds(ctx, "settlements", 1, 0, "bank-console", 1000, "merchant:1")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.ReviewFundsRequest(ctx, v.ID, true, "admin:1"); e != nil {
		t.Fatal(e)
	}
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "settlement-http-app", PrivateKey: key, PublicKey: &key.PublicKey})
	if e != nil {
		t.Fatal(e)
	}
	provider := &settlementBankHTTPProvider{settlementHTTPProvider: settlementHTTPProvider{Provider: ali}}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: provider}, AllowLive: true}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/admin"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	base := fmt.Sprintf("/settlement-payouts/%d", v.ID)
	w := invoke("GET", base+"/capabilities", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"bank_product":"alipay_bankcard"`) || !strings.Contains(w.Body.String(), `"can_prepare":true`) {
		t.Fatal("bank product omitted", w.Code, w.Body.String())
	}
	for _, step := range []struct{ suffix, body string }{{"prepare", `{"channel_id":"7"}`}, {"execute", `{}`}, {"query", `{}`}} {
		w = invoke("POST", base+"/"+step.suffix, step.body)
		if w.Code != 200 {
			t.Fatal(step.suffix, w.Code, w.Body.String())
		}
	}
	if provider.writes.Load() != 1 || !strings.Contains(w.Body.String(), `"state":"paid_fee_held"`) {
		t.Fatal("bank settlement fee hold hidden", w.Body.String())
	}
	checkReport := func(paid, net string) {
		t.Helper()
		w := invoke("GET", "/reports/transfer-merchants", "")
		var result struct {
			Data struct{ Items []map[string]string }
		}
		if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil || w.Code != 200 || len(result.Data.Items) != 1 {
			t.Fatal(w.Code, w.Body.String(), e)
		}
		r := result.Data.Items[0]
		if r["paid_money"] != paid || r["net_money"] != net || r["fee_money"] != "0.00" {
			t.Fatal("held fee counted as earned income", r)
		}
	}
	checkReport("9.75", "9.75")
	provider.returned = true
	w = invoke("POST", base+"/query", `{}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"returned"`) || !strings.Contains(w.Body.String(), `"state":"reversed"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	checkReport("0.00", "0.00")
	b, e := s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 1000 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
	w = invoke("POST", base+"/execute", `{}`)
	if w.Code != 200 || provider.writes.Load() != 1 {
		t.Fatal("returned original resent", w.Code, w.Body.String())
	}
}
