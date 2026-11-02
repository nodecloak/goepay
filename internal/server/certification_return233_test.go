package server

import (
	"goepay/internal/identity"
	"goepay/internal/kyc233"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestKYC233PublicReturnRejectsAmbiguityAndDoesNotEchoCode(t *testing.T) {
	for _, q := range []string{"", "attempt_id=a&state=b&auth_code=private-auth-code", "attempt_id=" + strings.Repeat("a", 32) + "&state=" + strings.Repeat("b", 32) + "&auth_code=private-auth-code&state=" + strings.Repeat("b", 32), "bad=%ZZ", "attempt_id=" + strings.Repeat("a", 32) + "&state=" + strings.Repeat("b", 32) + "&auth_code=private-auth-code&redirect_uri=https://evil.invalid"} {
		r := httptest.NewRequest("GET", "/callbacks/certification233/return?"+q, nil)
		w := httptest.NewRecorder()
		(&Server{}).Handler().ServeHTTP(w, r)
		if w.Code != 400 || strings.Contains(w.Body.String(), "private-auth-code") || w.Header().Get("Location") != "" {
			t.Fatal("invalid return not safely rejected", w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("callback privacy headers missing")
		}
	}
	w := httptest.NewRecorder()
	(&Server{}).Handler().ServeHTTP(w, httptest.NewRequest("POST", "/callbacks/certification233/return", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET" {
		t.Fatal("callback accepts mutation methods", w.Code)
	}
}

func TestKYC233PublicReturnSurvivesLoginBoundaryWithoutCertifyingOrCharging(t *testing.T) {
	f := newKYC233(t)
	f.s.KYC233ConsoleOrigin = "https://console.example.invalid"
	f.managedAlipay(t)
	in := f.settings(t)
	in.Mode = 3
	in.ChannelID = "7"
	kyc233View[kyc233.ConfigView](t, f.call("PUT", "admin", "/settings/certification", in))
	f.provider.initResult = kyc233.InitResult{Token: "synthetic-preconsult233", URL: "https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?scope=id_verify", Scan: "alipay", Status: "pending"}
	ready := kyc233View[kyc233.Info](t, f.call("POST", "merchant", "/account/certificate/submit", submission233()))
	kyc233View[kyc233.Info](t, f.call("POST", "merchant", "/account/certificate/geturl", operation233(ready.AttemptID)))
	callback := f.provider.lastAttempt
	u, e := url.Parse(callback.CallbackURL)
	if e != nil {
		t.Fatal(e)
	}
	if u.Path != "/callbacks/certification233/return" || u.Query().Get("state") != "" {
		t.Fatal("OAuth callback must be server-owned without duplicate state")
	}
	f.s.Sessions.Delete(f.merchantToken)
	q := u.Query()
	q.Set("state", callback.State)
	q.Set("auth_code", "synthetic-once-code233")
	q.Set("app_id", "2021000000000233")
	q.Set("scope", "id_verify")
	q.Set("source", "alipay")
	request := httptest.NewRequest("GET", u.Path+"?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, request)
	if w.Code != 303 || w.Header().Get("Location") != "https://console.example.invalid/merchant/account/certificate" || strings.Contains(w.Body.String(), "synthetic-once-code233") {
		t.Fatal("return not cleanly redirected", w.Code)
	}
	if f.provider.queryCalls.Load() != 0 {
		t.Fatal("public return called identity provider")
	}
	var count int
	if e = f.p.QueryRow(t.Context(), "SELECT count(*) FROM merchant_certifications233").Scan(&count); e != nil || count != 0 {
		t.Fatal("return certified without merchant", e)
	}
	f.merchantToken, f.merchantCSRF, e = f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "merchant233", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	before := f.call("GET", "merchant", "/account/certificate/info", nil)
	if !strings.Contains(before.Body.String(), `"auth_code_ready":true`) || strings.Contains(before.Body.String(), "synthetic-once-code233") {
		t.Fatal("inbox not privately resumable", before.Body.String())
	}
	done := kyc233View[kyc233.Info](t, f.call("POST", "merchant", "/account/certificate/query", operation233(ready.AttemptID)))
	if done.Cert != 1 || f.provider.queryCalls.Load() != 1 {
		t.Fatal("resumed query failed or repeated provider exchange")
	}
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, httptest.NewRequest("GET", request.URL.String(), nil))
	if w.Code == 303 {
		t.Fatal("used code revived by public replay")
	}
	if f.provider.queryCalls.Load() != 1 {
		t.Fatal("public replay called provider")
	}
}

func TestKYC233ReturnUsesOnlyTrustedConsoleOrigin(t *testing.T) {
	for _, tc := range []struct {
		origin, want string
		ok           bool
	}{
		{"", "/merchant/account/certificate", true},
		{"http://127.0.0.1:8181", "http://127.0.0.1:8181/merchant/account/certificate", true},
		{"https://console.example.invalid/", "https://console.example.invalid/merchant/account/certificate", true},
		{"https://bad.invalid/path", "", false}, {"https://user:pass@bad.invalid", "", false},
		{"https://bad.invalid?redirect=x", "", false}, {"https://bad.invalid#x", "", false},
		{"javascript:alert(1)", "", false}, {"//bad.invalid", "", false},
	} {
		s := Server{KYC233ConsoleOrigin: tc.origin}
		got, ok := s.certificationConsoleReturn233()
		if got != tc.want || ok != tc.ok {
			t.Fatalf("invalid trusted console boundary: %q %q %t", tc.origin, got, ok)
		}
	}
}
