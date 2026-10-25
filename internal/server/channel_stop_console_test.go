package server

import (
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChannelStop76ConsoleRequiresOriginalSuperCookieAndCSRF(t *testing.T) {
	for _, mode := range []string{"operator", "merchant", "delegated", "missing-cookie", "duplicate-cookie", "csrf"} {
		t.Run(mode, func(t *testing.T) {
			role := "admin"
			session := identity.Session{User: identity.User{ID: 7601, Role: role, Username: "local76", AdminClass: "super"}, CSRF: "local-csrf"}
			if mode == "operator" {
				session.User.AdminClass = "operator"
			}
			if mode == "merchant" {
				role = "merchant"
				session.User.Role = role
			}
			if mode == "delegated" {
				session.Issuer = &identity.SessionIssuer{ID: 1}
			}
			r := httptest.NewRequest("POST", "/console/v1/"+role+"/channel-stops/7/resume", strings.NewReader(`{"expected_event_id":"1","expected_revision":"1"}`))
			if mode != "missing-cookie" {
				r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: strings.Repeat("t", 43)})
			}
			if mode == "duplicate-cookie" {
				r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: strings.Repeat("t", 43)})
			}
			if mode != "csrf" {
				r.Header.Set("X-CSRF-Token", session.CSRF)
			}
			w := httptest.NewRecorder()
			if !(&Server{}).channelStopConsole(w, r, role, session, []string{role, "channel-stops", "7", "resume"}, strings.Repeat("t", 43)) || w.Code != 403 {
				t.Fatal("channel restoration ownership bypass", mode, w.Code, w.Body.String())
			}
		})
	}
}
