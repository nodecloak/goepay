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

func TestRefundSummaryUXOriginalFieldsFullFilterScopeAndReadOnly(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	for _, owner := range []int64{1, 2} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: owner, OutTradeNo: "summary-original", Name: "退款统计", Type: map[int64]string{1: "alipay", 2: "wxpay"}[owner], Amount: 1000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, fmt.Sprintf("summary-paid-%d", owner)); e != nil {
			t.Fatal(e)
		}
		for _, item := range []struct {
			no, state string
			amount    int64
		}{{"summary-succeeded", "succeeded", 100}, {"summary-pending", "pending", 50}, {"summary-unknown", "unknown", 30}, {"summary-failed", "failed", 20}} {
			if owner == 2 && item.state != "succeeded" {
				continue
			}
			r, e := s.Store.ReserveRefund(ctx, owner, o.OutTradeNo, fmt.Sprintf("%s-%d", item.no, owner), item.amount)
			if e != nil {
				t.Fatal(e)
			}
			if item.state != "pending" {
				if e = s.Store.ResolveRefund(ctx, r.ID, item.state); e != nil {
					t.Fatal(e)
				}
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
		r := httptest.NewRequest(method, "/console/v1/"+role+"/refunds/summary"+query, nil)
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
		{"admin", admin, "", map[string]string{"totalCount": "5", "totalMoney": "3.00", "successCount": "2", "successMoney": "2.00", "pendingCount": "2", "pendingMoney": "0.80"}},
		{"merchant", merchant, "", map[string]string{"totalCount": "4", "totalMoney": "2.00", "successCount": "1", "successMoney": "1.00", "pendingCount": "2", "pendingMoney": "0.80"}},
		{"admin", admin, "?merchant_id=1&status=unknown", map[string]string{"totalCount": "1", "totalMoney": "0.30", "successCount": "0", "successMoney": "0.00", "pendingCount": "1", "pendingMoney": "0.30"}},
		{"admin", admin, "?type=wxpay", map[string]string{"totalCount": "1", "totalMoney": "1.00", "successCount": "1", "successMoney": "1.00", "pendingCount": "0", "pendingMoney": "0.00"}},
		{"merchant", merchant, "?q=summary-failed", map[string]string{"totalCount": "1", "totalMoney": "0.20", "successCount": "0", "successMoney": "0.00", "pendingCount": "0", "pendingMoney": "0.00"}},
		{"admin", admin, "?from=1999-01-01&to=1999-01-01", map[string]string{"totalCount": "0", "totalMoney": "0.00", "successCount": "0", "successMoney": "0.00", "pendingCount": "0", "pendingMoney": "0.00"}},
	} {
		w := get("GET", tc.role, tc.token, tc.query)
		var out struct{ Data map[string]string }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("summary %d %s", w.Code, w.Body.String())
		}
		for key, want := range tc.want {
			if out.Data[key] != want {
				t.Fatalf("%s %s: %s got %s want %s", tc.role, tc.query, key, out.Data[key], want)
			}
		}
	}
	for _, q := range []string{"?uid=2", "?page=2", "?limit=1", "?format=csv", "?merchant_id=01", "?status=paid", "?type=other", "?channel_id=01", "?from=2026-01-02&to=2026-01-01", "?q=a&q=b", "?status=pending&status=unknown", "?q=%ZZ"} {
		if w := get("GET", "admin", admin, q); w.Code != 400 {
			t.Fatalf("invalid summary query %s %d %s", q, w.Code, w.Body.String())
		}
	}
	if w := get("GET", "merchant", merchant, "?merchant_id=2"); w.Code != 403 {
		t.Fatalf("foreign scope %d", w.Code)
	}
	if w := get("POST", "admin", admin, ""); w.Code != 405 {
		t.Fatalf("write summary method %d", w.Code)
	}
	var after, audits int
	if e = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM journals),(SELECT count(*) FROM backoffice_audit)").Scan(&after, &audits); e != nil || after != journalBefore || audits != auditBefore {
		t.Fatalf("stats wrote business state %d %d %v", after, audits, e)
	}
}
