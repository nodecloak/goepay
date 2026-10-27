package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOriginalSettlementFiltersUseOriginalRecipientStateAndScopedStatistics(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	for _, mid := range []int64{1, 2} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("settlement156-income-%d", mid), Amount: 10000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, "settlement156-paid:"+o.TradeNo); e != nil {
			t.Fatal(e)
		}
	}
	for i, in := range []struct {
		mid             int64
		kind            string
		amount          int64
		reject, approve bool
	}{{1, "alipay", 100, false, false}, {1, "wxpay", 200, true, false}, {1, "bank", 300, false, false}, {2, "alipay", 400, false, false}, {1, "alipay", 500, false, true}} {
		recipient := payment.SettlementRecipientInput{Type: in.kind, Account: "local156@example.invalid", Name: "Local Original"}
		if in.kind == "wxpay" {
			recipient.Account = "local-openid156"
			recipient.AppID = "local-app156"
		}
		if in.kind == "bank" {
			recipient.Account = "6210000000000156"
			recipient.BankCode = "1002"
		}
		if _, e := s.Store.SetSettlementRecipient(ctx, in.mid, recipient, nil, payment.SettlementRecipientActor{Role: "merchant", ID: in.mid, Name: "local"}); e != nil {
			t.Fatal(e)
		}
		r, e := s.Store.RequestFunds(ctx, "settlements", in.mid, 0, fmt.Sprintf("settlement156-%d", i), in.amount, "merchant")
		if e != nil {
			t.Fatal(e)
		}
		if in.reject || in.approve {
			if _, e = s.Store.ReviewFundsRequest(ctx, r.ID, in.approve, "admin:1:local"); e != nil {
				t.Fatal(e)
			}
		}
	}
	admin, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(role, path, q string) map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/workflows/settlements"+path+"?"+q, nil)
		token := admin
		if role == "merchant" {
			token = merchant
		}
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out struct{ Data map[string]any }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("filtered settlement %s %s: %d %s", path, q, w.Code, w.Body.String())
		}
		return out.Data
	}
	fingerprint := func() string {
		var out string
		if e := pool.QueryRow(ctx, `SELECT md5(jsonb_build_array((SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM accounts t),(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM ledger_requests t),(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM journals t),(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM backoffice_audit t))::text)`).Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	before := fingerprint()
	for _, tc := range []struct{ role, q, total, money string }{{"admin", "uid=1&type=1&dstatus=0", "2", "6.00"}, {"admin", "merchant_id=1&type=wxpay&dstatus=3", "1", "2.00"}, {"admin", "uid=2&type=1", "1", "4.00"}, {"merchant", "type=4&dstatus=0", "1", "3.00"}, {"merchant", "dstatus=1", "0", "0.00"}, {"admin", "type=3", "0", "0.00"}} {
		data := call(tc.role, "", tc.q+"&page=1&limit=1")
		var n int
		fmt.Sscan(tc.total, &n)
		if data["total"] != float64(n) {
			t.Fatalf("unscoped count %s %+v", tc.q, data)
		}
		stats := call(tc.role, "/statistics", tc.q)
		if stats["total_count"] != tc.total || stats["total_money"] != tc.money {
			t.Fatalf("wrong original statistics %s %+v", tc.q, stats)
		}
		if n > 0 {
			if len(data["items"].([]any)) != 1 {
				t.Fatal("pagination not independent from statistics")
			}
			for _, item := range data["items"].([]any) {
				if tc.role == "merchant" && item.(map[string]any)["merchant_id"] != "1" {
					t.Fatal("foreign tenant")
				}
			}
		}
	}
	today := time.Now().In(time.FixedZone("Beijing", 8*3600)).Format("2006-01-02")
	if got := call("admin", "/statistics", "starttime="+today+"&endtime="+today); got["total_count"] != "5" || got["completed_count"] != "0" || got["waiting_count"] != "4" || got["failed_count"] != "1" {
		t.Fatalf("approval incorrectly completed %+v", got)
	}
	if got := call("admin", "/statistics", "from=2000-01-01&to=2000-01-01"); got["total_count"] != "0" {
		t.Fatal("date ignored", got)
	}
	if fingerprint() != before {
		t.Fatal("read-only settlement filters/statistics wrote financial or audit state")
	}
}

func TestOriginalSettlementFiltersRejectInvalidAliasesAndForeignMerchant(t *testing.T) {
	s, _, _ := fixture(t)
	tokens := map[string]string{}
	for _, role := range []string{"admin", "merchant"} {
		token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: role, Role: role, AdminClass: "super"})
		if e != nil {
			t.Fatal(e)
		}
		tokens[role] = token
	}
	call := func(role, path, q string) int {
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/workflows/settlements"+path+"?"+q, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: tokens[role]})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	for _, path := range []string{"", "/statistics"} {
		for _, q := range []string{"uid=01", "uid=1&merchant_id=2", "type=1&type=2", "type=7", "type=alipay%00", "dstatus=01", "dstatus=-1", "dstatus=7", "from=2026-02-30&to=2026-03-01", "starttime=2026-01-01&endtime=2025-01-01", "from=2026-01-01", "from=2026-01-01&starttime=2026-01-02&to=2026-01-03", "unknown=1"} {
			if got := call("admin", path, q); got != 422 {
				t.Fatalf("invalid query %s %q got %d", path, q, got)
			}
		}
		if got := call("merchant", path, "uid=2"); got != 403 {
			t.Fatal("foreign merchant scope accepted", path, got)
		}
	}
}
