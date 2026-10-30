package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// 资金明细 统计 goes through the whole console router and the real ledger
// tables: its count and 增加 / 减少 / 总计 must equal what the ledger list shows
// for the same filter, per owner.
func TestLedgerSummaryRouteMatchesLedgerListPerOwner(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	for i, item := range []struct {
		owner, amount, refund int64
	}{{1, 1000, 100}, {1, 500, 0}, {2, 300, 0}} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: item.owner, OutTradeNo: fmt.Sprintf("ledger-summary-%d", i), Name: "资金明细统计", Type: "alipay", Amount: item.amount})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, fmt.Sprintf("ledger-summary-paid-%d", i)); e != nil {
			t.Fatal(e)
		}
		if item.refund > 0 {
			r, e := s.Store.ReserveRefund(ctx, item.owner, o.OutTradeNo, fmt.Sprintf("ledger-summary-refund-%d", i), item.refund)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.Store.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
				t.Fatal(e)
			}
		}
	}
	admin, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, role, token, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/"+role+path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	yuan := func(minor int64) string {
		sign := ""
		if minor < 0 {
			sign, minor = "-", -minor
		}
		return fmt.Sprintf("%s%d.%02d", sign, minor/100, minor%100)
	}
	for _, tc := range []struct{ role, token, query string }{
		{"admin", admin, ""},
		{"merchant", merchant, ""},
		{"admin", admin, "?merchant_id=2"},
	} {
		sep := "?"
		if tc.query != "" {
			sep = "&"
		}
		lw := call("GET", tc.role, tc.token, "/ledger"+tc.query+sep+"limit=100")
		var list struct {
			Data struct {
				Items []map[string]any
				Total json.Number
			}
		}
		if lw.Code != 200 || json.Unmarshal(lw.Body.Bytes(), &list) != nil {
			t.Fatalf("%s ledger list %q: %d %s", tc.role, tc.query, lw.Code, lw.Body.String())
		}
		var inc, dec, net int64
		for _, item := range list.Data.Items {
			v, e := strconv.ParseInt(fmt.Sprint(item["amount"]), 10, 64)
			if e != nil {
				t.Fatalf("ledger amount %v: %v", item["amount"], e)
			}
			if v > 0 {
				inc += v
			} else {
				dec -= v
			}
			net += v
		}
		if len(list.Data.Items) == 0 {
			t.Fatalf("%s %q: fixture produced no ledger rows", tc.role, tc.query)
		}
		sw := call("GET", tc.role, tc.token, "/ledger/summary"+tc.query)
		var out struct{ Data map[string]string }
		if sw.Code != 200 || json.Unmarshal(sw.Body.Bytes(), &out) != nil {
			t.Fatalf("%s ledger summary %q: %d %s", tc.role, tc.query, sw.Code, sw.Body.String())
		}
		want := map[string]string{"totalCount": list.Data.Total.String(), "increaseMoney": yuan(inc), "decreaseMoney": yuan(dec), "netMoney": yuan(net)}
		for k, v := range want {
			if out.Data[k] != v {
				t.Fatalf("%s %q: %s got %q want %q (list rows %d)", tc.role, tc.query, k, out.Data[k], v, len(list.Data.Items))
			}
		}
	}
	if w := call("GET", "merchant", merchant, "/ledger/summary?merchant_id=2"); w.Code != 403 {
		t.Fatalf("foreign ledger scope %d %s", w.Code, w.Body.String())
	}
	for _, q := range []string{"?page=2", "?limit=1", "?merchant_id=01", "?from=2026-13-01"} {
		if w := call("GET", "admin", admin, "/ledger/summary"+q); w.Code != 400 {
			t.Fatalf("invalid ledger summary query %s: %d %s", q, w.Code, w.Body.String())
		}
	}
	if w := call("POST", "admin", admin, "/ledger/summary"); w.Code != 405 {
		t.Fatalf("write ledger summary %d %s", w.Code, w.Body.String())
	}
}
