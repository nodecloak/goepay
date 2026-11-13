package server

import (
	"encoding/json"
	"goepay/internal/cryptopay"
	"goepay/internal/identity"
	"net/http/httptest"
	"testing"
)

func TestCryptoCheckoutDisabledDoesNotFallThroughToLegacyPayment(t *testing.T) {
	for _, suffix := range []string{"", "/status", "/qr"} {
		r := httptest.NewRequest("GET", "/crypto/checkout/local-invoice"+suffix+"?token=untrusted", nil)
		w := httptest.NewRecorder()
		(&Server{}).Handler().ServeHTTP(w, r)
		if w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("disabled crypto route %s: status=%d cache=%q", suffix, w.Code, w.Header().Get("Cache-Control"))
		}
	}
}

func TestCryptoDisabledNetworksDoNotEnableConsoleMutations(t *testing.T) {
	s := &Server{Crypto: &cryptopay.Service{Networks: map[string]cryptopay.NetworkConfig{"tron": {Enabled: false}, "ethereum": {Enabled: false}}}}
	w := httptest.NewRecorder()
	s.cryptoConsole(w, httptest.NewRequest("GET", "/console/v1/merchant/crypto/config", nil), "merchant", identity.Session{}, []string{"merchant", "crypto", "config"}, "")
	var v struct{ Data struct{ Enabled bool } }
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || v.Data.Enabled {
		t.Fatal("closed networks enabled console writes", w.Body.String(), e)
	}
}

func TestCryptoCheckoutRejectsMutationMethods(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).Handler().ServeHTTP(w, httptest.NewRequest("POST", "/crypto/checkout/local-invoice/status?token=untrusted", nil))
	if w.Code != 405 {
		t.Fatalf("public checkout accepted mutation: %d", w.Code)
	}
}
