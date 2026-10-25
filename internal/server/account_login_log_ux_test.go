package server

import (
	"goepay/internal/identity"
	"net/http/httptest"
	"testing"
)

func TestAccountLoginLogUXRejectDelegationMethodsAndMalformedQueriesBeforeStore(t *testing.T) {
	for _, tc := range []struct {
		method, query, role string
		delegated           bool
		status              int
	}{
		{"GET", "", "admin", false, 403},
		{"GET", "", "merchant", true, 403},
		{"POST", "", "merchant", false, 405},
		{"GET", "?merchant_id=2", "merchant", false, 400},
		{"GET", "?limit=0", "merchant", false, 400},
		{"GET", "?page=01", "merchant", false, 400},
		{"GET", "?page=1&page=2", "merchant", false, 400},
	} {
		session := identity.Session{User: identity.User{Role: tc.role, ID: 1}}
		if tc.delegated {
			session.Issuer = &identity.SessionIssuer{ID: 1}
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, "/console/v1/"+tc.role+"/account/login-log"+tc.query, nil)
		if !(&Server{}).account(w, r, tc.role, session, []string{tc.role, "account", "login-log"}) || w.Code != tc.status {
			t.Fatalf("owner log guard %s %s delegated=%t got %d want %d", tc.role, tc.query, tc.delegated, w.Code, tc.status)
		}
	}
}

func TestAdminMerchantLoginLogUXRejectsInvalidAuthorityAndScopeBeforeStore(t *testing.T) {
	for _, tc := range []struct {
		role, class, id, method, query string
		delegated                      bool
		status                         int
	}{
		{"merchant", "", "1", "GET", "", false, 403},
		{"admin", "operator", "1", "GET", "", true, 403},
		{"admin", "unknown", "1", "GET", "", false, 403},
		{"admin", "super", "01", "GET", "", false, 400},
		{"admin", "super", "1", "POST", "", false, 405},
		{"admin", "operator", "1", "GET", "?merchant_id=2", false, 400},
		{"admin", "super", "1", "GET", "?limit=01", false, 400},
	} {
		session := identity.Session{User: identity.User{Role: tc.role, ID: 1, AdminClass: tc.class}}
		if tc.delegated {
			session.Issuer = &identity.SessionIssuer{ID: 1}
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, "/console/v1/"+tc.role+"/merchants/"+tc.id+"/login-log"+tc.query, nil)
		if !(&Server{}).adminMerchantLoginLog(w, r, tc.role, session, []string{tc.role, "merchants", tc.id, "login-log"}) || w.Code != tc.status {
			t.Fatalf("admin log guard %+v got %d", tc, w.Code)
		}
	}
}
