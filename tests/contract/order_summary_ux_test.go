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
)

// 订单统计概况 goes through the whole console router. GET /orders/summary must
// reach the summary handler, not the order-detail route, which would read
// "summary" as an order number and answer "order not found".
func TestOrderSummaryUXRouteCountsCurrentFilterAndStaysReadOnly(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	for i, item := range []struct {
		owner      int64
		kind       string
		amount     int64
		paid       bool
		refundedBy int64
	}{
		{1, "alipay", 1000, true, 100},
		{1, "alipay", 500, true, 0},
		{1, "alipay", 200, false, 0},
		{2, "wxpay", 300, true, 0},
	} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: item.owner, OutTradeNo: fmt.Sprintf("order-summary-%d", i), Name: "订单统计", Type: item.kind, Amount: item.amount})
		if e != nil {
			t.Fatal(e)
		}
		if !item.paid {
			continue
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, fmt.Sprintf("order-summary-paid-%d", i)); e != nil {
			t.Fatal(e)
		}
		if item.refundedBy > 0 {
			r, e := s.Store.ReserveRefund(ctx, item.owner, o.OutTradeNo, fmt.Sprintf("order-summary-refund-%d", i), item.refundedBy)
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
	get := func(method, role, token, query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/"+role+"/orders/summary"+query, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	var journalBefore, auditBefore int
	if e = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM journals),(SELECT count(*) FROM backoffice_audit)").Scan(&journalBefore, &auditBefore); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		role, token, query string
		want               map[string]string
	}{
		{"admin", admin, "", map[string]string{"totalCount": "4", "paidCount": "3", "unpaidCount": "1", "refundedCount": "1", "successRate": "75.00%", "orderMoney": "20.00", "paidMoney": "18.00", "refundedMoney": "1.00"}},
		{"merchant", merchant, "", map[string]string{"totalCount": "3", "paidCount": "2", "unpaidCount": "1", "refundedCount": "1", "successRate": "66.67%", "orderMoney": "17.00", "paidMoney": "15.00", "refundedMoney": "1.00"}},
		{"admin", admin, "?type=wxpay", map[string]string{"totalCount": "1", "paidCount": "1", "unpaidCount": "0", "refundedCount": "0", "successRate": "100.00%", "orderMoney": "3.00", "paidMoney": "3.00", "refundedMoney": "0.00"}},
		{"admin", admin, "?merchant_id=2", map[string]string{"totalCount": "1", "paidCount": "1", "orderMoney": "3.00"}},
		{"admin", admin, "?from=1999-01-01&to=1999-01-01", map[string]string{"totalCount": "0", "paidCount": "0", "unpaidCount": "0", "refundedCount": "0", "successRate": "—", "orderMoney": "0.00", "paidMoney": "0.00", "merchantMoney": "0.00", "refundedMoney": "0.00"}},
	} {
		w := get("GET", tc.role, tc.token, tc.query)
		var out struct{ Data map[string]string }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("%s summary %q: %d %s", tc.role, tc.query, w.Code, w.Body.String())
		}
		for key, want := range tc.want {
			if out.Data[key] != want {
				t.Fatalf("%s %q: %s got %q want %q", tc.role, tc.query, key, out.Data[key], want)
			}
		}
		if _, ok := out.Data["merchantMoney"]; !ok {
			t.Fatalf("%s %q: merchantMoney missing", tc.role, tc.query)
		}
	}
	for _, q := range []string{"?page=2", "?limit=1", "?q=a&q=b", "?type=other", "?merchant_id=01", "?from=0000-01-01"} {
		if w := get("GET", "admin", admin, q); w.Code != 400 {
			t.Fatalf("invalid summary query %s: %d %s", q, w.Code, w.Body.String())
		}
	}
	if w := get("GET", "merchant", merchant, "?merchant_id=2"); w.Code != 403 {
		t.Fatalf("foreign scope %d %s", w.Code, w.Body.String())
	}
	if w := get("POST", "admin", admin, ""); w.Code != 405 {
		t.Fatalf("write summary method %d %s", w.Code, w.Body.String())
	}
	var journalAfter, auditAfter int
	if e = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM journals),(SELECT count(*) FROM backoffice_audit)").Scan(&journalAfter, &auditAfter); e != nil || journalAfter != journalBefore || auditAfter != auditBefore {
		t.Fatalf("summary wrote business state %d->%d %d->%d %v", journalBefore, journalAfter, auditBefore, auditAfter, e)
	}
}
