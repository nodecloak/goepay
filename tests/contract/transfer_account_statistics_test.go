package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"testing"
)

// transferAccountStatProvider is a minimal alipay+wxpay transfer provider that
// immediately succeeds, so seeded payouts land in a terminal 'succeeded' state
// for the 付款账号统计 aggregate without any external call.
type transferAccountStatProvider struct {
	adapter.Provider
	id string
}

func (p *transferAccountStatProvider) ID() string              { return p.id }
func (p *transferAccountStatProvider) TransferTypes() []string { return []string{p.id} }
func (p *transferAccountStatProvider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	return adapter.TransferResult{TransferNo: r.TransferNo, Amount: r.Amount, State: "SUCCESS", ProviderTransferNo: p.id + "-tn"}, nil
}
func (p *transferAccountStatProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	return adapter.TransferResult{TransferNo: r.TransferNo, Amount: r.Amount, State: "SUCCESS", ProviderTransferNo: p.id + "-tn"}, nil
}

// TestTransferAccountStatisticsBreaksDownByPayeeAccount is the P0 parity fix:
// /transfer/stat must group by the payee account (masked), not the old
// hard-coded '未知'. It also covers the 付款方式 (type) filter and admin-only
// access.
func TestTransferAccountStatisticsBreaksDownByPayeeAccount(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	// Fund merchant 1 with enough available balance and enable transfers.
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"0"}' WHERE id=1; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "acctstat-fund", Amount: 100000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "acctstat-fund"); e != nil {
		t.Fatal(e)
	}
	alipay := &transferAccountStatProvider{id: "alipay"}
	wxpay := &transferAccountStatProvider{id: "wxpay"}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: alipay, 8: wxpay}, AllowLive: true}
	s.MerchantChannels = map[int64][]int64{1: {7, 8}}

	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	create := func(channel, typ, account, name, amount, no string) {
		t.Helper()
		body := `{"channel_id":"` + channel + `","type":"` + typ + `","account":"` + account + `","name":"` + name + `","amount":"` + amount + `","remark":"本地统计","external_no":"` + no + `"}`
		r := httptest.NewRequest("POST", "/console/v1/merchant/transfers", bytes.NewBufferString(body))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 && w.Code != 202 {
			t.Fatalf("create %s failed: %d %s", no, w.Code, w.Body.String())
		}
	}
	// Two alipay payouts to the SAME account, one to a different alipay account,
	// one wxpay payout — four payouts, three distinct payee accounts.
	create("7", "alipay", "payee-one@test", "Payee One", "10.00", "s-a1")
	create("7", "alipay", "payee-one@test", "Payee One", "5.00", "s-a2")
	create("7", "alipay", "payee-two@test", "Payee Two", "7.00", "s-b1")
	create("8", "wxpay", "wx-openid-zzz", "WX Payee", "3.00", "s-c1")

	adminToken, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	get := func(path, cookie, tok string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		if tok != "" {
			r.AddCookie(&http.Cookie{Name: cookie, Value: tok})
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	// Admin: unfiltered — three payee-account rows.
	code, out := get("/console/v1/admin/reports/transfer-accounts", "goepay_admin", adminToken)
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, out)
	}
	data := out["data"].(map[string]any)
	if data["total"] != float64(3) {
		t.Fatalf("expected 3 payee-account rows, got total=%v", data["total"])
	}
	cols := data["columns"].([]any)
	if cols[0].(map[string]any)["label"] != "付款账号" || cols[1].(map[string]any)["label"] != "姓名" {
		t.Fatalf("columns not account-keyed: %v", cols)
	}
	items := data["items"].([]any)
	// The top row by paid money is the account with 10.00+5.00 = 15.00, 2 笔.
	top := items[0].(map[string]any)
	if top["paid_money"] != "15.00" || top["orders"] != "2" || top["paid_orders"] != "2" {
		t.Fatalf("account aggregate wrong: %v", top)
	}
	if top["dimension"] == "" || top["dimension"] == "未知" || top["dimension"] == "未记录" {
		t.Fatalf("account dimension not a masked account: %v", top["dimension"])
	}
	// Masking policy: the real name never appears (settlement masking).
	if top["name"] == "Payee One" || top["name"] == "" {
		t.Fatalf("payee name must be masked, not raw or empty: %q", top["name"])
	}

	// Type filter — wxpay only yields the single wxpay payee account.
	code, out = get("/console/v1/admin/reports/transfer-accounts?type=wxpay", "goepay_admin", adminToken)
	if code != 200 {
		t.Fatalf("wxpay filter status=%d body=%v", code, out)
	}
	data = out["data"].(map[string]any)
	if data["total"] != float64(1) {
		t.Fatalf("wxpay filter expected 1 row, got %v", data["total"])
	}
	if data["items"].([]any)[0].(map[string]any)["paid_money"] != "3.00" {
		t.Fatalf("wxpay filter money wrong: %v", data["items"])
	}

	// Invalid method rejected.
	code, _ = get("/console/v1/admin/reports/transfer-accounts?type=nope", "goepay_admin", adminToken)
	if code != 400 {
		t.Fatalf("invalid method accepted: %d", code)
	}

	// Admin-only: a merchant session is refused by the shared reports gate.
	code, _ = get("/console/v1/merchant/reports/transfer-accounts", "goepay_merchant", token)
	if code != 403 {
		t.Fatalf("merchant reached admin transfer-accounts report: %d", code)
	}

	// CSV export carries the masked account rows.
	r := httptest.NewRequest("GET", "/console/v1/admin/reports/transfer-accounts?format=csv", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: adminToken})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/csv; charset=utf-8" {
		t.Fatalf("csv export failed: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("付款账号")) {
		t.Fatalf("csv missing account header: %s", w.Body.String())
	}
}
