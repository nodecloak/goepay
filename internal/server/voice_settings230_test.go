package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVoice230SettingsRequireCredentialsAndNeverEchoThem(t *testing.T) {
	f := channel168Local(t)
	call := func(method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/admin/settings/voice", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	view := func(w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var out struct {
			Data map[string]any `json:"data"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("voice settings HTTP %d: %s", w.Code, w.Body.String())
		}
		return out.Data
	}
	first := view(call("GET", ""))
	if first["enabled"] != false || first["secret_present"] != false {
		t.Fatal("new voice settings must start disabled and empty")
	}
	invalid := call("PUT", `{"expected_revision":"0","enabled":true,"credential_action":"keep","app_id":"","secret":""}`)
	if invalid.Code != 422 {
		t.Fatalf("missing agent credentials must reject enable, got %d", invalid.Code)
	}
	set := view(call("PUT", `{"expected_revision":"0","enabled":true,"credential_action":"replace","app_id":"local-agent230","secret":"synthetic-secret230"}`))
	if set["enabled"] != true || set["secret_present"] != true || set["revision"] != "1" {
		t.Fatal("voice settings not saved")
	}
	if strings.Contains(call("GET", "").Body.String(), "synthetic-secret230") {
		t.Fatal("secret echoed")
	}
	var encrypted []byte
	if err := f.p.QueryRow(t.Context(), "SELECT credential_cipher FROM voice_settings230").Scan(&encrypted); err != nil || strings.Contains(string(encrypted), "synthetic-secret230") {
		t.Fatal("credentials not encrypted", err)
	}
	for _, body := range []string{`{"expected_revision":"1","enabled":null,"credential_action":"keep","app_id":"","secret":""}`, `{"expected_revision":"1","credential_action":"keep","app_id":"","secret":""}`} {
		if got := call("PUT", body).Code; got != 400 {
			t.Fatalf("ambiguous settings accepted: %d", got)
		}
	}
	conflict := call("PUT", `{"expected_revision":"0","enabled":false,"credential_action":"keep","app_id":"","secret":""}`)
	if conflict.Code != 409 {
		t.Fatalf("stale revision accepted: %d", conflict.Code)
	}
	badClear := call("PUT", `{"expected_revision":"1","enabled":true,"credential_action":"clear","app_id":"","secret":""}`)
	if badClear.Code != 422 {
		t.Fatal("enabled configuration allowed clear")
	}
	cleared := view(call("PUT", `{"expected_revision":"1","enabled":false,"credential_action":"clear","app_id":"","secret":""}`))
	if cleared["enabled"] != false || cleared["secret_present"] != false {
		t.Fatal("disable and clear failed")
	}
	var audit string
	if err := f.p.QueryRow(t.Context(), `SELECT string_agg(changes::text,'') FROM backoffice_audit WHERE resource='voice-settings230'`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(audit, "synthetic-secret230") || strings.Contains(audit, "local-agent230") {
		t.Fatal("voice credentials leaked into audit")
	}
	if _, err := f.p.Exec(t.Context(), "DELETE FROM voice_settings230"); err != nil {
		t.Fatal(err)
	}
	if got := call("GET", "").Code; got != 409 {
		t.Fatalf("missing managed source silently became legacy: %d", got)
	}
}
