package server

import (
	"goepay/internal/identity"
	"net/http/httptest"
	"testing"
)

func TestRefundSummaryUXMalformedScopeAndQueriesFailBeforeStore(t *testing.T) {
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
		{"GET", "admin", "?from=0000-01-01", 1, 400},
		{"GET", "merchant", "?merchant_id=2", 1, 403},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, "/console/v1/"+tc.role+"/refunds/summary"+tc.query, nil)
		session := identity.Session{User: identity.User{ID: tc.id, Role: tc.role}}
		if !(&Server{}).refundSummary(w, r, tc.role, session, []string{tc.role, "refunds", "summary"}) || w.Code != tc.status {
			t.Fatalf("refund summary guard %+v got %d", tc, w.Code)
		}
	}
}
