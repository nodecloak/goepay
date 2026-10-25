package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
)

func TestPrintingConsoleRequiresOwnMerchantOrOwnSuper(t *testing.T) {
	for _, tc := range []struct {
		role, path, class string
		delegated         bool
	}{
		{"admin", "printing/settings", "operator", false},
		{"merchant", "printing/settings", "", false},
		{"admin", "account/printing", "super", false},
		{"admin", "printing/settings", "super", true},
		{"merchant", "account/printing", "", true},
		{"admin", "orders/123/receipt", "super", false},
		{"merchant", "print-jobs", "", true},
	} {
		session := identity.Session{User: identity.User{ID: 1, Username: "local", Role: tc.role, AdminClass: tc.class}, CSRF: "csrf"}
		if tc.delegated {
			session.Issuer = &identity.SessionIssuer{ID: 1}
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/console/v1/"+tc.role+"/"+tc.path, nil)
		if !(&Server{}).printingConsole(w, r, tc.role, session, append([]string{tc.role}, strings.Split(tc.path, "/")...)) || w.Code != 403 {
			t.Fatalf("printing ownership bypass %s/%s: %d", tc.role, tc.path, w.Code)
		}
	}
}

func TestPrintingConsoleRejectsCSRFAndTenantOverridesBeforeStore(t *testing.T) {
	session := identity.Session{User: identity.User{ID: 1, Username: "owner", Role: "merchant"}, CSRF: "csrf"}
	for _, tc := range []struct {
		method, path, query string
		status              int
	}{
		{"PUT", "account/printing", "", 403},
		{"POST", "orders/123/print-jobs", "", 403},
		{"GET", "account/printing", "?merchant_id=2", 400},
		{"GET", "print-jobs", "?after_id=0&limit=20&limit=30", 400},
		{"GET", "print-jobs", "?after_id=00", 400},
		{"DELETE", "print-jobs/1", "", 403},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, "/console/v1/merchant/"+tc.path+tc.query, strings.NewReader(`{}`))
		if !(&Server{}).printingConsole(w, r, "merchant", session, append([]string{"merchant"}, strings.Split(tc.path, "/")...)) || w.Code != tc.status {
			t.Fatalf("printing validation bypass %s%s: %d", tc.path, tc.query, w.Code)
		}
	}
}
