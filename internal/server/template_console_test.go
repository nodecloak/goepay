package server

import (
	"goepay/internal/identity"
	"net/http/httptest"
	"testing"
)

func TestTemplatePublicWithoutStoreReturnsUnavailable(t *testing.T) {
	for _, path := range []string{"/", "/template-assets/" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		w := httptest.NewRecorder()
		if !(&Server{}).templatePublic(w, httptest.NewRequest("GET", path, nil)) || w.Code != 503 {
			t.Fatalf("%s: got %d, want 503", path, w.Code)
		}
	}
}

func TestTemplateConsoleRequiresOwnSuperAndCSRF(t *testing.T) {
	for _, session := range []identity.Session{{User: identity.User{ID: 1, Username: "m", Role: "merchant"}}, {User: identity.User{ID: 1, Username: "op", Role: "admin", AdminClass: "operator"}}, {User: identity.User{ID: 1, Username: "super", Role: "admin", AdminClass: "super"}, Issuer: &identity.SessionIssuer{ID: 9}}} {
		r := httptest.NewRequest("GET", "/console/v1/admin/templates?type=pay", nil)
		w := httptest.NewRecorder()
		if !(&Server{}).templateConsole(w, r, "admin", session, []string{"admin", "templates"}) || w.Code != 403 {
			t.Fatal("unprivileged template API accepted", w.Code)
		}
	}
	s := identity.Session{User: identity.User{ID: 1, Username: "s", Role: "admin", AdminClass: "super"}, CSRF: "c"}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/console/v1/admin/templates/pay/selection", nil)
	if !(&Server{}).templateConsole(w, r, "admin", s, []string{"admin", "templates", "pay", "selection"}) || w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
}
