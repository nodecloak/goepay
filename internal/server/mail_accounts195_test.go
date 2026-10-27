package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mail195Request(f channel168Fixture, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/settings/mail-channels"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func mail195Data(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("mail account response invalid or cached", w.Code)
	}
	var out struct {
		Code int
		Data map[string]any
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Code != 0 || out.Data == nil {
		t.Fatal("mail account envelope invalid", e)
	}
	if strings.Contains(w.Body.String(), `"password":`) {
		t.Fatal("SMTP password field echoed")
	}
	return out.Data
}

func TestMailAccounts195OriginalSMTPFieldsProtectedSaveAndNoSend(t *testing.T) {
	f := channel168Local(t)
	w := mail195Request(f, "GET", "", "")
	if w.Code != 200 {
		t.Fatal("original mail channel catalog missing", w.Code)
	}
	list := mail195Data(t, w)
	items, ok := list["items"].([]any)
	if !ok || len(items) != 1 || items[0].(map[string]any)["name"] != "smtp" || items[0].(map[string]any)["title"] != "SMTP发信" {
		t.Fatal("original SMTP catalog row missing", list)
	}
	state := mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
	if state["configured"] != false || state["password_set"] != false || state["test_available"] != false {
		t.Fatal("unconfigured or default-off state misleading", state)
	}
	const secret = "synthetic195-unique-password-not-for-provider"
	body, _ := json.Marshal(map[string]any{"name": "smtp", "status": 1, "config": map[string]string{"host": "smtp.example.invalid", "port": "465", "username": "sender195@example.invalid", "password": secret}, "expected_fingerprint": state["fingerprint"]})
	w = mail195Request(f, "PATCH", "/smtp", string(body))
	if w.Code != 200 {
		t.Fatal("original SMTP account save missing", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), `"password":`) {
		t.Fatal("SMTP secret echoed")
	}
	state = mail195Data(t, w)
	config, ok := state["config"].(map[string]any)
	if !ok || config["host"] != "smtp.example.invalid" || config["port"] != "465" || config["username"] != "sender195@example.invalid" || state["password_set"] != true || state["status"] != float64(1) || state["configured"] != true {
		t.Fatal("original SMTP fields not persisted", state)
	}
	if w = mail195Request(f, "PATCH", "/smtp", string(body)); w.Code != 409 {
		t.Fatal("stale SMTP account save accepted", w.Code)
	}
	body, _ = json.Marshal(map[string]any{"name": "smtp", "status": 0, "config": map[string]string{"host": "smtp.example.invalid", "port": "465", "username": "sender195@example.invalid"}, "keep_password": true, "expected_fingerprint": state["fingerprint"]})
	state = mail195Data(t, mail195Request(f, "PATCH", "/smtp", string(body)))
	if state["status"] != float64(0) || state["password_set"] != true {
		t.Fatal("disable lost original stored password", state)
	}
	var effects int
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM orders)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)+(SELECT count(*)FROM enrollment_challenges)`).Scan(&effects); e != nil || effects != 0 {
		t.Fatal("account save sent a message or touched money", effects, e)
	}
	var leaked bool
	if e := f.p.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM mail_account_revisions195 WHERE facts::text LIKE '%'||$1::text||'%') OR EXISTS(SELECT 1 FROM backoffice_audit WHERE changes::text LIKE '%'||$1::text||'%')`, secret).Scan(&leaked); e != nil || leaked {
		t.Fatal("SMTP password stored in plaintext source/audit", leaked, e)
	}
}
