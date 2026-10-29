package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// With the SMS gateway off (the default) the public sms-code endpoint is inert:
// it answers 503 and persists no verification code, so register/recover behave
// exactly as before and no existing test changes.
func TestPublicSMSCode134InertWhenGatewayOff(t *testing.T) {
	f := channel168Local(t)
	r := httptest.NewRequest("POST", "/console/v1/public/sms-code",
		strings.NewReader(`{"purpose":"register","phone":"13800138000"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal("sms-code must be inert (503) with the gateway off", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("sms-code response must not be cached")
	}
	var codes int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*)FROM verification_codes134`).Scan(&codes); e != nil || codes != 0 {
		t.Fatal("no verification code may be issued while the gateway is off", codes, e)
	}
	// Bad method and unknown purpose are rejected before any issuance too.
	bad := httptest.NewRequest("GET", "/console/v1/public/sms-code", nil)
	bw := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(bw, bad)
	if bw.Code != 405 {
		t.Fatal("GET must be 405", bw.Code)
	}
	var h http.Header = bw.Header()
	if h.Get("Allow") != "POST" {
		t.Fatal("Allow header must be POST", h.Get("Allow"))
	}
}
