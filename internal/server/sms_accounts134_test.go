package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sms134Request(f channel168Fixture, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/settings/sms-channels"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func sms134Data(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("sms gateway response invalid or cached", w.Code, w.Body.String())
	}
	var out struct {
		Code int
		Data map[string]any
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Code != 0 || out.Data == nil {
		t.Fatal("sms gateway envelope invalid", e, w.Body.String())
	}
	return out.Data
}

func TestSMSGateway134ProviderSaveProtectedAndNoEcho(t *testing.T) {
	f := channel168Local(t)

	// GET lists the three original providers, gateway off, no active.
	state := sms134Data(t, sms134Request(f, "GET", "", ""))
	providers, ok := state["providers"].([]any)
	if !ok || len(providers) != 3 {
		t.Fatal("original three SMS providers missing", state)
	}
	names := map[string]bool{}
	for _, p := range providers {
		m := p.(map[string]any)
		names[m["provider"].(string)] = true
		if m["configured"] != false {
			t.Fatal("provider misleadingly configured", m)
		}
	}
	if !names["aliyun"] || !names["qcloud"] || !names["smsbao"] {
		t.Fatal("provider set is not aliyun/qcloud/smsbao", names)
	}
	if state["status"] != float64(0) || state["active"] != "" {
		t.Fatal("gateway should start disabled with no active provider", state)
	}

	// Enabling without a configured active provider is rejected.
	badBody, _ := json.Marshal(map[string]any{"active": "aliyun", "status": 1, "expected_fingerprint": state["fingerprint"]})
	if w := sms134Request(f, "PATCH", "", string(badBody)); w.Code != 422 {
		t.Fatal("enable without configured provider accepted", w.Code, w.Body.String())
	}

	// Save aliyun credentials + enable.
	const secret = "synthetic134-access-secret-not-for-provider"
	body, _ := json.Marshal(map[string]any{
		"provider": "aliyun",
		"credentials": map[string]string{
			"access_key_id": "LTAI-test", "access_key_secret": secret,
			"template": "SMS_TEST_1", "sign": "测试签名",
		},
		"active": "aliyun", "status": 1,
		"expected_fingerprint": state["fingerprint"],
	})
	w := sms134Request(f, "PATCH", "", string(body))
	if w.Code != 200 {
		t.Fatal("aliyun save rejected", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), "access_key_secret") {
		t.Fatal("SMS secret echoed to the browser")
	}
	state = sms134Data(t, w)
	if state["status"] != float64(1) || state["active"] != "aliyun" {
		t.Fatal("gateway not enabled on aliyun", state)
	}
	var aliyun map[string]any
	for _, p := range state["providers"].([]any) {
		if m := p.(map[string]any); m["provider"] == "aliyun" {
			aliyun = m
		}
	}
	if aliyun["configured"] != true || aliyun["template"] != "SMS_TEST_1" || aliyun["sign"] != "测试签名" {
		t.Fatal("aliyun non-secret fields not persisted", aliyun)
	}
	if ak, _ := aliyun["principal"].(string); ak == "LTAI-test" || !strings.Contains(ak, "****") {
		t.Fatal("principal not masked", aliyun["principal"])
	}

	// Stale fingerprint is rejected (CAS).
	if w := sms134Request(f, "PATCH", "", string(body)); w.Code != 409 {
		t.Fatal("stale fingerprint save accepted", w.Code)
	}

	// Secret never persisted in plaintext (sealed) nor in audit.
	var leaked bool
	if e := f.p.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM sms_providers134 WHERE convert_from(sealed,'UTF8') LIKE '%'||$1||'%') OR EXISTS(SELECT 1 FROM backoffice_audit WHERE changes::text LIKE '%'||$1||'%')`, secret).Scan(&leaked); e == nil && leaked {
		t.Fatal("SMS secret stored in plaintext source/audit")
	}

	// Disabling the gateway is allowed and keeps the saved provider.
	state2 := sms134Data(t, sms134Request(f, "GET", "", ""))
	offBody, _ := json.Marshal(map[string]any{"active": "aliyun", "status": 0, "expected_fingerprint": state2["fingerprint"]})
	state2 = sms134Data(t, sms134Request(f, "PATCH", "", string(offBody)))
	if state2["status"] != float64(0) {
		t.Fatal("gateway disable not persisted", state2)
	}

	// No money/orders touched by SMS config.
	var effects int
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM orders)+(SELECT count(*)FROM journals)`).Scan(&effects); e != nil || effects != 0 {
		t.Fatal("sms config touched money", effects, e)
	}
}
