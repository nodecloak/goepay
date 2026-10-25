package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPreauthorizationLegacyCheckoutRequiresExistingExpiryToken(t *testing.T) {
	s := &Server{CheckoutKey: []byte(strings.Repeat("k", 32))}
	for _, path := range []string{"/pay/preauth/guessable/", "/pay/preauth/guessable/?expires=1&token=forged", "/pay/preauth/guessable/extra/"} {
		w := httptest.NewRecorder()
		s.preauthorizationCheckout(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 403 && w.Code != 404 {
			t.Fatal("public authorization checkout accepted", path, w.Code)
		}
	}
	link := s.checkoutURL("authorized", "checkout")
	query := strings.SplitN(link, "?", 2)[1]
	w := httptest.NewRecorder()
	s.preauthorizationCheckout(w, httptest.NewRequest("GET", "/pay/preauth/authorized/?"+query, nil))
	if w.Code != 302 || !strings.HasPrefix(w.Header().Get("Location"), "/checkout/authorized?") {
		t.Fatal("valid token lost in legacy alias", w.Code, w.Header())
	}
}
func TestPreauthorizationCallbackRejectsMethodsAndMissingFlow(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.preauthorizationCallback(w, httptest.NewRequest("GET", "/pay/preauthnotify/known/", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	s.preauthorizationCallback(w, httptest.NewRequest("POST", "/pay/preauthnotify/known/", strings.NewReader("signed=unknown")))
	if w.Code != 503 || strings.TrimSpace(w.Body.String()) != "fail" {
		t.Fatal(w.Code, w.Body.String())
	}
}
