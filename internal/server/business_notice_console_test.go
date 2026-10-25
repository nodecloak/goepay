package server

import (
	"goepay/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminBusiness83ConsoleRestrictsRoleDelegationMethodsAndCanonicalCursor(t *testing.T) {
	for _, tc := range []struct {
		role, class, method, query string
		delegated                  bool
		want                       int
	}{
		{"admin", "super", "GET", "", false, 503}, {"admin", "operator", "GET", "?before_id=0&limit=20", false, 503}, {"admin", "auditor", "GET", "", false, 403}, {"merchant", "operator", "GET", "", false, 403}, {"admin", "super", "GET", "", true, 403},
		{"admin", "super", "POST", "", false, 405}, {"admin", "super", "GET", "?merchant_id=1000", false, 400}, {"admin", "super", "GET", "?limit=20&limit=30", false, 400}, {"admin", "super", "GET", "?before_id=00", false, 400}, {"admin", "super", "GET", "?limit=101", false, 400}, {"admin", "super", "GET", "?limit=0", false, 400}, {"admin", "super", "GET", "?before_id=-1", false, 400}, {"admin", "super", "GET", "?limit=%ff", false, 400},
	} {
		t.Run(tc.role+tc.class+tc.method+tc.query, func(t *testing.T) {
			session := identity.Session{User: identity.User{ID: 1, Username: "synthetic83", Role: tc.role, AdminClass: tc.class}}
			if tc.delegated {
				session.Issuer = &identity.SessionIssuer{ID: 2}
			}
			r := httptest.NewRequest(tc.method, "/console/v1/"+tc.role+"/business-notices"+tc.query, strings.NewReader(""))
			out := httptest.NewRecorder()
			if !(&Server{}).businessNoticeConsole(out, r, tc.role, session, []string{tc.role, "business-notices"}) || out.Code != tc.want {
				t.Fatal("authority/cursor mismatch", out.Code, tc.want)
			}
		})
	}
}
