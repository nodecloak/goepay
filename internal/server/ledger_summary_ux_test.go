package server

import (
	"goepay/internal/identity"
	"net/http/httptest"
	"testing"
)

// The 资金明细 统计 totals must only ever cover rows the caller may list, so every
// malformed scope or query is refused before the store is touched (s.Pool is nil
// here; reaching it would panic).
func TestLedgerSummaryUXMalformedScopeAndQueriesFailBeforeStore(t *testing.T) {
	for _, tc := range []struct {
		method, role, query string
		id                  int64
		status              int
	}{
		{"GET", "merchant", "", 0, 403},
		{"POST", "admin", "", 1, 405},
		{"GET", "admin", "?page=2", 1, 400},
		{"GET", "admin", "?limit=10", 1, 400},
		{"GET", "admin", "?q=a&q=b", 1, 400},
		{"GET", "admin", "?q=%00", 1, 400},
		{"GET", "admin", "?type=other", 1, 400},
		{"GET", "admin", "?from=0000-01-01", 1, 400},
		{"GET", "admin", "?merchant_id=0", 1, 400},
		{"GET", "merchant", "?merchant_id=2", 1, 403},
		{"GET", "admin", "?status=paid", 1, 400},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, "/console/v1/"+tc.role+"/ledger/summary"+tc.query, nil)
		session := identity.Session{User: identity.User{ID: tc.id, Role: tc.role}}
		if !(&Server{}).ledgerSummary(w, r, tc.role, session, []string{tc.role, "ledger", "summary"}) || w.Code != tc.status {
			t.Fatalf("ledger summary guard %+v got %d", tc, w.Code)
		}
	}
}

// A path that is not the ledger summary must pass straight through (return false)
// so the ordinary dispatch chain continues.
func TestLedgerSummaryIgnoresOtherPaths(t *testing.T) {
	for _, parts := range [][]string{
		{"admin", "ledger"},
		{"admin", "refunds", "summary"},
		{"admin", "orders", "summary"},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/console/v1/admin/"+parts[1], nil)
		session := identity.Session{User: identity.User{ID: 1, Role: "admin"}}
		if (&Server{}).ledgerSummary(w, r, "admin", session, parts) {
			t.Fatalf("expected ledger summary to ignore %v", parts)
		}
	}
}
