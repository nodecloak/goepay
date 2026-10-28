package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/platform/httpclient"
)

func proxyTestCall(f channel168Fixture, token, csrf, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/console/v1/admin/settings/proxy-test", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func TestProxyConnectivityProbesThroughConfiguredServerWithStoredPassword(t *testing.T) {
	f := channel168Local(t)
	// Store a password through the write-only console first.
	empty := proxyCredView(t, proxyCredCall(f, f.token, f.csrf, "GET", ""))
	proxyCredView(t, proxyCredCall(f, f.token, f.csrf, "PATCH", `{"password":"relay-pwd","expected_fingerprint":"`+empty.Fingerprint+`"}`))
	var sawCfg httpclient.ProxyConfig
	f.s.ProxyConnectivity = func(_ context.Context, cfg httpclient.ProxyConfig) (string, error) {
		sawCfg = cfg
		return "198.51.100.77", nil
	}
	w := proxyTestCall(f, f.token, f.csrf, `{"type":"sock5","server":"proxy.example.net","port":"1080","user":"agent"}`)
	var out struct {
		Code int
		Data struct {
			Reachable bool   `json:"reachable"`
			IP        string `json:"ip"`
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || !out.Data.Reachable || out.Data.IP != "198.51.100.77" {
		t.Fatalf("connectivity result: HTTP=%d %s", w.Code, w.Body.String())
	}
	if sawCfg.Type != "sock5" || sawCfg.Server != "proxy.example.net" || sawCfg.Port != "1080" || sawCfg.User != "agent" || sawCfg.Password != "relay-pwd" {
		t.Fatalf("probe used the wrong config (password must come from the store): %+v", sawCfg)
	}
	if strings.Contains(w.Body.String(), "relay-pwd") {
		t.Fatal("connectivity response echoed the proxy password")
	}
}

func TestProxyConnectivityFailureAndGating(t *testing.T) {
	f := channel168Local(t)
	f.s.ProxyConnectivity = func(context.Context, httpclient.ProxyConfig) (string, error) {
		return "", context.DeadlineExceeded
	}
	ok := `{"type":"http","server":"proxy.example.net","port":"8080"}`
	if w := proxyTestCall(f, f.token, f.csrf, ok); w.Code != 200 || !strings.Contains(w.Body.String(), "代理连接失败") || strings.Contains(w.Body.String(), `"reachable":true`) {
		t.Fatalf("unreachable proxy: HTTP=%d %s", w.Code, w.Body.String())
	}
	for _, bad := range []string{`{"type":"socks5","server":"p","port":"8080"}`, `{"type":"http","server":"","port":"8080"}`, `{"type":"http","server":"p","port":"0"}`, `{"type":"http","server":"p","port":"x"}`} {
		if w := proxyTestCall(f, f.token, f.csrf, bad); w.Code != 422 {
			t.Fatalf("invalid proxy test input accepted: %s -> %d", bad, w.Code)
		}
	}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('proxytestop','admin',172,'Op','unused','operator')`); err != nil {
		t.Fatal(err)
	}
	opToken, opCSRF, err := f.s.Sessions.Create(identity.User{ID: 172, Role: "admin", Username: "proxytestop", AdminClass: "operator", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if w := proxyTestCall(f, opToken, opCSRF, ok); w.Code != 403 {
		t.Fatal("operator reached the proxy connectivity probe", w.Code)
	}
	if w := proxyTestCall(f, f.token, "", ok); w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
}
