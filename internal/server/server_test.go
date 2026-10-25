package server

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDecodeRejectsTrailingInvalidJSON(t *testing.T) {
	for _, raw := range []string{`{"name":"x"} trailing`, `{"name":"x"} {"name":"y"}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(raw))
		var in struct {
			Name string `json:"name"`
		}
		if decode(w, r, &in) {
			t.Errorf("accepted %q", raw)
		}
	}
}
func TestCallbackURLPolicy(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1/hook", "http://169.254.169.254/metadata", "https://[::ffff:127.0.0.1]/", "http://user@example.com/"} {
		if validURL(u) {
			t.Errorf("accepted %s", u)
		}
	}
	if !validURL("") || !validURL("https://example.com/hook") {
		t.Fatal("valid callback rejected")
	}
}
func TestWireUsesPHPScalarAndSDKBlankRules(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{PlatformKey: key}
	w := httptest.NewRecorder()
	s.wire(w, false, map[string]any{"sandbox": true, "space": " \t"}, "")
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	p := compat.Params{}
	for k, v := range out {
		p[k] = fmt.Sprint(v)
	}
	p["sandbox"] = "1"
	if !compat.VerifyRSAWithProfile(p, p["sign"], &key.PublicKey, compat.ProfileSDKV2) {
		t.Fatal("wire signature incompatible with PHP boolean/blank serialization")
	}
}
func TestLoginRateLimitSeparatedAndBounded(t *testing.T) {
	s := &Server{}
	now := time.Now()
	for i := 0; i < 10; i++ {
		if !s.allowLogin("127.0.0.1:1000", "admin", "a", now) {
			t.Fatal("early limit")
		}
	}
	if s.allowLogin("127.0.0.1:2000", "admin", "a", now) {
		t.Fatal("limit bypass by source port")
	}
	if !s.allowLogin("127.0.0.1:1000", "admin", "b", now) {
		t.Fatal("one username blocks another behind proxy")
	}
	if !s.allowLogin("127.0.0.1:1000", "merchant", "a", now) {
		t.Fatal("role not isolated")
	}
	if !s.allowLogin("127.0.0.1:1000", "admin", "a", now.Add(16*time.Minute)) {
		t.Fatal("expired attempts retained")
	}
	s.attempts = make(map[string]attempt, 10000)
	for i := 0; i < 10000; i++ {
		s.attempts[fmt.Sprint(i)] = attempt{count: 1, since: now}
	}
	if s.allowLogin("127.0.0.1:1000", "admin", "new", now) {
		t.Fatal("unbounded new key")
	}
	if len(s.attempts) > 10000 {
		t.Fatal("unbounded map")
	}
}
func TestCheckoutSimulationJumpOrigin(t *testing.T) {
	s := &Server{ProviderFlow: &payment.ProviderFlow{LocalSimulation: true, CallbackBaseURL: "http://127.0.0.1:8180"}}
	for _, raw := range []string{"http://127.0.0.1:8180/simulation/pay?a=1", "https://official.example/pay"} {
		if !s.checkoutJumpAllowed(raw) {
			t.Fatal("valid jump rejected")
		}
	}
	for _, raw := range []string{"http://127.0.0.1:8181/simulation/pay", "http://127.0.0.1:8180.evil.invalid/x", "http://user@127.0.0.1:8180/x", "http://evil.invalid/x"} {
		if s.checkoutJumpAllowed(raw) {
			t.Fatal("unsafe simulation jump", raw)
		}
	}
	s.ProviderFlow.LocalSimulation = false
	if s.checkoutJumpAllowed("http://127.0.0.1:8180/simulation/pay") {
		t.Fatal("non-simulation HTTP jump permitted")
	}
}
