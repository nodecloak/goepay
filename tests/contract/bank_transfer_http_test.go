package contract_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
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

type bankHTTPProvider struct {
	adapter.Provider
	state    atomic.Value
	feeKnown atomic.Bool
}

func (*bankHTTPProvider) TransferTypes() []string { return []string{"bank"} }
func (*bankHTTPProvider) ID() string              { return "wxpay" }
func (p *bankHTTPProvider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: "bank-http-1", State: "PROCESSING", Amount: r.Amount, BankFeeAmount: 2, BankFeeKnown: true}, nil
}
func (p *bankHTTPProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: "bank-http-1", State: p.state.Load().(string), Amount: r.Amount, BankFeeAmount: 2, BankFeeKnown: p.feeKnown.Load()}, nil
}
func TestBankTransferAPIAndConsoleDiscloseReturnInsteadOfStaleSuccess(t *testing.T) {
	s, pool, key := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"3"}' WHERE id=2; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "bank-http-fund", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "bank-http-fund"); e != nil {
		t.Fatal(e)
	}
	p := &bankHTTPProvider{}
	p.state.Store("SUCCESS")
	p.feeKnown.Store(true)
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true}
	s.MerchantChannels = map[int64][]int64{2: {7}}
	adminToken, _, _ := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1})
	checkReport := func(wantPaid, wantMoney string) {
		t.Helper()
		for _, name := range []string{"transfer-accounts", "transfer-channels", "transfer-merchants"} {
			labels := map[string]string{}
			for _, format := range []string{"", "?format=csv"} {
				r := httptest.NewRequest("GET", "/console/v1/admin/reports/"+name+format, nil)
				r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: adminToken})
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				if w.Code != 200 {
					t.Fatalf("report %s %d", name, w.Code)
				}
				if format != "" {
					rows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
					if e != nil || len(rows) != 2 {
						t.Fatalf("report CSV %v %d", e, len(rows))
					}
					// Column order differs per report (付款账号统计 adds 姓名), and the
					// generic report CSVs head columns with keys while 付款账号统计
					// uses labels, so locate 成功笔数 and 付款金额 by either.
					paidAt, moneyAt := -1, -1
					for i, h := range rows[0] {
						switch h {
						case "paid_orders", labels["paid_orders"]:
							paidAt = i
						case "paid_money", labels["paid_money"]:
							moneyAt = i
						}
					}
					if paidAt < 0 || moneyAt < 0 || rows[1][paidAt] != wantPaid || rows[1][moneyAt] != wantMoney {
						t.Fatalf("%s counts reversed as paid: %v %v", name, rows[0], rows[1])
					}
				} else {
					var out struct {
						Data struct {
							Columns []struct{ Key, Label string } `json:"columns"`
							Items   []map[string]string           `json:"items"`
						} `json:"data"`
					}
					if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || len(out.Data.Items) != 1 {
						t.Fatalf("report JSON %v", e)
					}
					for _, c := range out.Data.Columns {
						labels[c.Key] = c.Label
					}
					row := out.Data.Items[0]
					if row["paid_orders"] != wantPaid || row["paid_money"] != wantMoney {
						t.Fatalf("%s counts returned bank transfer as paid: %v", name, row)
					}
				}
			}
		}
	}
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
	first := invoke("/api/transfer/submit", compat.Params{"type": "bank", "account": "6222020202020202020", "name": "Synthetic Person", "bank_code": "1002", "money": "1.00", "out_biz_no": "bank-http", "channel_id": "7"})
	if first["code"] != float64(0) || first["status"] != float64(0) {
		t.Fatal(first)
	}
	current := invoke("/api/transfer/query", compat.Params{"out_biz_no": "bank-http"})
	if current["status"] != float64(1) || current["bank_lifecycle"] != "succeeded" || current["bank_fee_amount"] != "0.02" {
		t.Fatal(current)
	}
	checkReport("1", "1.00")
	p.state.Store("BANK_FAIL")
	p.feeKnown.Store(false)
	current = invoke("/api/transfer/query", compat.Params{"out_biz_no": "bank-http"})
	if current["status"] != float64(0) || current["bank_lifecycle"] != "reversal_pending" {
		t.Fatal("unknown bank return shown successful", current)
	}
	checkReport("0", "0.00")
	b, _ := s.Store.Balance(ctx, 2)
	if b.Available != 897 {
		t.Fatal("unverified bank return credited", b)
	}
	p.feeKnown.Store(true)
	current = invoke("/api/transfer/query", compat.Params{"out_biz_no": "bank-http"})
	if current["status"] != float64(2) || current["bank_lifecycle"] != "reversed" || current["ledger_status"] != "succeeded" || len(current["bank_reversal_journals"].([]any)) != 3 {
		t.Fatal("bank return hidden", current)
	}
	checkReport("0", "0.00")
	token, _, _ := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1})
	r := httptest.NewRequest("GET", "/console/v1/admin/transfers", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var out struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil || w.Code != 200 || len(out.Data.Items) != 1 || out.Data.Items[0]["status"] != "reversed" || out.Data.Items[0]["ledger_status"] != "succeeded" {
		t.Fatal("console bank return hidden", w.Body.String(), e)
	}
}
