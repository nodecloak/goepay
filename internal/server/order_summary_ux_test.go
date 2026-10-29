package server

import (
	"goepay/internal/identity"
	"net/http/httptest"
	"testing"
)

// The order 统计概况 overview must reject a malformed scope, wrong method and
// cross-tenant access before it ever reaches the store, exactly like the refund
// summary — the counts it returns must only ever cover rows the caller may list.
func TestOrderSummaryUXMalformedScopeAndQueriesFailBeforeStore(t *testing.T) {
	for _, tc := range []struct {
		method, role, query string
		id                  int64
		status              int
	}{
		{"GET", "merchant", "", 0, 403},
		{"POST", "admin", "", 1, 405},
		{"GET", "admin", "?page=2", 1, 400},
		{"GET", "admin", "?q=a&q=b", 1, 400},
		{"GET", "admin", "?q=%00", 1, 400},
		{"GET", "admin", "?type=other", 1, 400},
		{"GET", "admin", "?status=other", 1, 400},
		{"GET", "admin", "?from=0000-01-01", 1, 400},
		{"GET", "admin", "?search_field=mobile&q=138", 1, 400},
		{"GET", "merchant", "?merchant_id=2", 1, 403},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, "/console/v1/"+tc.role+"/orders/summary"+tc.query, nil)
		session := identity.Session{User: identity.User{ID: tc.id, Role: tc.role}}
		if !(&Server{}).orderSummary(w, r, tc.role, session, []string{tc.role, "orders", "summary"}) || w.Code != tc.status {
			t.Fatalf("order summary guard %+v got %d", tc, w.Code)
		}
	}
}

// The order summary route owns only /orders/summary; anything else is passed on.
func TestOrderSummaryIgnoresOtherPaths(t *testing.T) {
	for _, parts := range [][]string{{"admin", "orders"}, {"admin", "orders", "list"}, {"admin", "refunds", "summary"}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/console/v1/admin/x", nil)
		session := identity.Session{User: identity.User{ID: 1, Role: "admin"}}
		if (&Server{}).orderSummary(w, r, "admin", session, parts) {
			t.Fatalf("order summary claimed foreign path %v", parts)
		}
	}
}
