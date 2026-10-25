package server

import (
	"goepay/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReferralConsoleRejectsAdminFakeOwnerDelegationAndPolicyOperator(t *testing.T) {
	for _, tc := range []struct {
		role   string
		user   identity.User
		issuer *identity.SessionIssuer
		path   string
		want   int
	}{
		{"admin", identity.User{ID: 1, Username: "a", Role: "admin", AdminClass: "super"}, nil, "referral/overview", 403},
		{"merchant", identity.User{ID: 1, Username: "m", Role: "merchant"}, &identity.SessionIssuer{ID: 1}, "referral/token", 403},
		{"merchant", identity.User{ID: 1, Username: "m", Role: "merchant"}, nil, "referral-policies/global/0", 403},
		{"admin", identity.User{ID: 1, Username: "a", Role: "admin", AdminClass: "operator"}, nil, "referral-policies/global/0", 403},
	} {
		s := &Server{}
		r := httptest.NewRequest("GET", "/console/v1/"+tc.role+"/"+tc.path, nil)
		w := httptest.NewRecorder()
		if !s.referralConsole(w, r, tc.role, identity.Session{User: tc.user, Issuer: tc.issuer, CSRF: "csrf"}, append([]string{tc.role}, strings.Split(tc.path, "/")...)) || w.Code != tc.want {
			t.Fatalf("owner/RBAC bypass status=%d want=%d", w.Code, tc.want)
		}
	}
}
func TestReferralConsoleRequiresCSRFForTokenWritesAndStrictPolicyPath(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("POST", "/console/v1/merchant/referral/token/rotate", strings.NewReader(`{"expected":"0"}`))
	w := httptest.NewRecorder()
	if !s.referralConsole(w, r, "merchant", identity.Session{User: identity.User{ID: 1, Username: "m", Role: "merchant"}, CSRF: "csrf"}, []string{"merchant", "referral", "token", "rotate"}) || w.Code != 403 {
		t.Fatal("token mutation without CSRF accepted")
	}
}
