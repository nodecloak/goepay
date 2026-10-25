package server

import (
	"goepay/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettlementBatches84ConsoleOwnRoleCSRFAndCanonicalInputs(t *testing.T) {
	for _, tc := range []struct {
		role, class, method, path, query string
		delegated                        bool
		want                             int
	}{
		{"admin", "super", "GET", "", "", false, 503},
		{"admin", "operator", "GET", "", "?limit=20&before_id=0", false, 503},
		{"merchant", "operator", "GET", "", "", false, 403},
		{"admin", "auditor", "GET", "", "", false, 403},
		{"admin", "super", "GET", "", "", true, 403},
		{"admin", "super", "POST", "", "", false, 403},
		{"admin", "operator", "GET", "/1/export", "?format=common", false, 403},
		{"admin", "super", "GET", "", "?limit=00", false, 400},
		{"admin", "super", "GET", "", "?limit=1&limit=2", false, 400},
		{"admin", "super", "GET", "", "?merchant_id=1", false, 400},
		{"admin", "super", "GET", "/01", "", false, 404},
		{"admin", "super", "GET", "/1/export", "?format=anything", false, 400},
		{"admin", "super", "GET", "/1/export", "?format=common&format=wxpay", false, 400},
	} {
		t.Run(tc.role+tc.class+tc.method+tc.path+tc.query, func(t *testing.T) {
			session := identity.Session{User: identity.User{ID: 1, Username: "local84", Role: tc.role, AdminClass: tc.class}, CSRF: "csrf84"}
			if tc.delegated {
				session.Issuer = &identity.SessionIssuer{ID: 2}
			}
			r := httptest.NewRequest(tc.method, "/console/v1/"+tc.role+"/settlement-batches"+tc.path+tc.query, strings.NewReader("{}"))
			w := httptest.NewRecorder()
			if !(&Server{}).settlementBatchConsole(w, r, tc.role, session, append([]string{tc.role, "settlement-batches"}, strings.FieldsFunc(tc.path, func(r rune) bool { return r == '/' })...)) || w.Code != tc.want {
				t.Fatal(w.Code, tc.want, w.Body.String())
			}
		})
	}
}
