package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type transferHTTPProvider struct {
	adapter.Provider
	writes  atomic.Int32
	unknown bool
}

func (p *transferHTTPProvider) TransferTypes() []string { return []string{"alipay"} }
func (*transferHTTPProvider) ID() string                { return "alipay" }
func (p *transferHTTPProvider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.writes.Add(1)
	if p.unknown {
		return adapter.TransferResult{}, &adapter.UnknownOutcome{Operation: "transfer", Cause: errors.New("lost response")}
	}
	return adapter.TransferResult{TransferNo: r.TransferNo, Amount: r.Amount, State: "PROCESSING", ProviderTransferNo: "ali-transfer-1"}, nil
}

func TestConsoleTransferOwnershipUnknownRecoveryAndActorAudit(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"3"}' WHERE id=1; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "console-transfer-funds", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "console-transfer-funds"); e != nil {
		t.Fatal(e)
	}
	provider := &transferHTTPProvider{unknown: true}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: provider}, AllowLive: true}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(method, "/console/v1/merchant/transfers"+path, bytes.NewBufferString(body))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(w.Body.String(), e)
		}
		return w.Code, out
	}
	code, out := invoke("GET", "/capabilities", "")
	if code != 200 || out["data"].(map[string]any)["writes_enabled"] != true {
		t.Fatal(code, out)
	}
	bad := `{"merchant_id":"2","channel_id":"7","type":"alipay","account":"private-account","amount":"1.00","external_no":"console-outgoing"}`
	if code, _ = invoke("POST", "", bad); code != 403 {
		t.Fatal("cross merchant create", code)
	}
	body := `{"channel_id":"7","type":"alipay","account":"private-account","name":"private-name","amount":"1.00","external_no":"console-outgoing"}`
	if _, e = pool.Exec(ctx, `CREATE FUNCTION reject_transfer_actor() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END $$; CREATE TRIGGER reject_transfer_actor BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_transfer_actor()`); e != nil {
		t.Fatal(e)
	}
	if code, _ = invoke("POST", "", body); code == 200 || code == 202 || provider.writes.Load() != 0 {
		t.Fatal("upstream call despite audit failure", code)
	}
	if _, e = pool.Exec(ctx, "DROP TRIGGER reject_transfer_actor ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	code, out = invoke("POST", "", body)
	if code != 202 {
		t.Fatal(code, out)
	}
	transfer := out["data"].(map[string]any)
	id := transfer["id"].(string)
	balance, _ := s.Store.Balance(ctx, 1)
	if balance.Available != 897 || balance.Reserved != 103 {
		t.Fatal("unknown lost reserve", balance)
	}
	var count int
	var audit string
	if e = pool.QueryRow(ctx, "SELECT count(*),COALESCE(string_agg(changes::text,','),'') FROM backoffice_audit WHERE resource='transfers' AND actor_role='merchant' AND actor_id=1 AND actor_name='merchant'").Scan(&count, &audit); e != nil || count != 2 || strings.Contains(audit, "private-account") || strings.Contains(audit, "private-name") {
		t.Fatal("actor audit missing or leaked identity", count, audit, e)
	}
	code, out = invoke("GET", "/"+id, "")
	if code != 200 || out["data"].(map[string]any)["recipient"].(map[string]any)["account"] != "private-account" {
		t.Fatal("owned transfer detail", code, out)
	}
	s.ProviderFlow.AllowLive = false
	code, out = invoke("POST", "/"+id+"/query", `{}`)
	if code != 200 || out["data"].(map[string]any)["status"] != "succeeded" {
		t.Fatal(code, out)
	}
	balance, _ = s.Store.Balance(ctx, 1)
	if balance.Available != 897 || balance.Reserved != 0 {
		t.Fatal(balance)
	}
	if provider.writes.Load() != 1 {
		t.Fatal("unknown write repeated")
	}
}
func (p *transferHTTPProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	return adapter.TransferResult{TransferNo: r.TransferNo, Amount: r.Amount, State: "SUCCESS", ProviderTransferNo: "ali-transfer-1", PayDate: "2026-10-23 10:00:00"}, nil
}
func TestTransferSignedHTTPSubmitQueryAndAuthorization(t *testing.T) {
	s, pool, key := fixture(t)
	ctx := context.Background()
	pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"3"}' WHERE id=2`)
	pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`)
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "transfer-funds", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "transfer-funds"); e != nil {
		t.Fatal(e)
	}
	provider := &transferHTTPProvider{}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: provider}, AllowLive: true}
	s.MerchantChannels = map[int64][]int64{2: {7}}
	invoke := func(path string, p compat.Params) map[string]any {
		t.Helper()
		p["pid"] = "2"
		p["timestamp"] = fmt.Sprint(time.Now().Unix())
		p["sign_type"] = "RSA"
		p["sign"], _ = compat.SignRSA(p, key)
		v := url.Values{}
		for k, x := range p {
			v.Set(k, x)
		}
		_, out := call(t, s.Handler(), "POST", path, v)
		return out
	}
	params := func() compat.Params {
		return compat.Params{"type": "alipay", "money": "1.00", "account": "private-payee@example.invalid", "name": "Test Payee", "remark": "test", "out_biz_no": "transfer-1", "channel_id": "7"}
	}
	bad := params()
	bad["channel_id"] = "8"
	if out := invoke("/api/transfer/submit", bad); out["code"] != float64(-1) {
		t.Fatal("unauthorizedchannel", out)
	}
	first := invoke("/api/transfer/submit", params())
	if first["code"] != float64(0) || first["status"] != float64(0) || first["cost_money"] != "1.03" {
		t.Fatal(first)
	}
	if again := invoke("/api/transfer/submit", params()); again["biz_no"] != first["biz_no"] || provider.writes.Load() != 1 {
		t.Fatal("transfer duplicate", again)
	}
	s.ProviderFlow.AllowLive = false
	out := invoke("/api/transfer/query", compat.Params{"biz_no": fmt.Sprint(first["biz_no"])})
	if out["status"] != float64(1) || out["orderid"] != "ali-transfer-1" {
		t.Fatal(out)
	}
	balance, _ := s.Store.Balance(ctx, 2)
	if balance.Available != 897 || balance.Reserved != 0 {
		t.Fatal(balance)
	}
	forged := params()
	forged["account"] = "attacker"
	s.ProviderFlow.AllowLive = true
	if out = invoke("/api/transfer/submit", forged); out["code"] != float64(-1) {
		t.Fatal("recipient changed", out)
	}
	if provider.writes.Load() != 1 {
		t.Fatal("transfer repeated")
	}
}
