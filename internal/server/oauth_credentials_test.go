package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/connectconfig"
	"goepay/internal/identity"
)

func oauthCredCall(f channel168Fixture, token, csrf, method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/settings/oauth-credentials", strings.NewReader(body))
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

func oauthCredView(t *testing.T, w *httptest.ResponseRecorder) connectconfig.View {
	t.Helper()
	var out struct {
		Code int                `json:"code"`
		Data connectconfig.View `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("oauth credential call failed: HTTP=%d %s", w.Code, w.Body.String())
	}
	return out.Data
}

func TestOAuthCredentialsStoreWriteOnlyAndCAS(t *testing.T) {
	f := channel168Local(t)
	// Empty store: nothing set, a stable fingerprint, no plaintext.
	empty := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
	if empty.QQSet || empty.WXSet || empty.AlipaySet || empty.TelegramSet || len(empty.Fingerprint) != 64 {
		t.Fatalf("empty view wrong: %+v", empty)
	}
	// Set the QQ appkey.
	set := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"qq","value":"qq-app-secret-xyz","expected_fingerprint":"`+empty.Fingerprint+`"}`))
	if !set.QQSet || set.WXSet || set.Fingerprint == empty.Fingerprint {
		t.Fatalf("set view wrong: %+v", set)
	}
	// The secret is never echoed.
	if w := oauthCredCall(f, f.token, f.csrf, "GET", ""); strings.Contains(w.Body.String(), "qq-app-secret-xyz") {
		t.Fatal("secret leaked through the console")
	}
	// A stale fingerprint is refused (CAS).
	if w := oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"qq","clear":true,"expected_fingerprint":"`+empty.Fingerprint+`"}`); w.Code != 409 {
		t.Fatal("stale fingerprint accepted", w.Code)
	}
	// Set the Telegram bot token too; QQ stays.
	set2 := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"telegram","value":"123456:bot-token-abc","expected_fingerprint":"`+set.Fingerprint+`"}`))
	if !set2.QQSet || !set2.TelegramSet {
		t.Fatalf("second set wrong: %+v", set2)
	}
	// Server-internal Resolve returns the decrypted secrets, no actor.
	store := f.s.connectConfigStore()
	if store == nil {
		t.Fatal("store unavailable")
	}
	if v, e := store.Resolve(t.Context(), "qq"); e != nil || v != "qq-app-secret-xyz" {
		t.Fatalf("resolve qq wrong: %q %v", v, e)
	}
	if v, e := store.Resolve(t.Context(), "telegram"); e != nil || v != "123456:bot-token-abc" {
		t.Fatalf("resolve telegram wrong: %q %v", v, e)
	}
	if v, e := store.Resolve(t.Context(), "wx"); e != nil || v != "" {
		t.Fatalf("unset wx must resolve empty: %q %v", v, e)
	}
	// Clear QQ; Telegram stays.
	cleared := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"qq","clear":true,"expected_fingerprint":"`+set2.Fingerprint+`"}`))
	if cleared.QQSet || !cleared.TelegramSet {
		t.Fatalf("selective clear wrong: %+v", cleared)
	}
	// The ciphertext on disk is not the plaintext.
	var qq, tg []byte
	if err := f.p.QueryRow(t.Context(), "SELECT qq_cipher,tg_cipher FROM connect_login_credentials WHERE id=true").Scan(&qq, &tg); err != nil {
		t.Fatal(err)
	}
	if len(qq) != 0 || strings.Contains(string(tg), "bot-token-abc") {
		t.Fatal("secret stored in plaintext or cleared value lingers")
	}
}

func TestOAuthCredentialsRequireSuperAdminAndCSRF(t *testing.T) {
	f := channel168Local(t)
	if _, err := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('oauthop','admin',181,'OAuth operator','unused-synthetic','operator')`); err != nil {
		t.Fatal(err)
	}
	opToken, opCSRF, err := f.s.Sessions.Create(identity.User{ID: 181, Role: "admin", Username: "oauthop", AdminClass: "operator", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	empty := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
	cases := []struct {
		name, token, csrf, body string
		method                  string
		want                    int
	}{
		{"operator-read", opToken, opCSRF, "", "GET", 403},
		{"operator-write", opToken, opCSRF, `{"field":"qq","value":"x","expected_fingerprint":"` + empty.Fingerprint + `"}`, "PATCH", 403},
		{"no-csrf", f.token, "", `{"field":"qq","value":"x","expected_fingerprint":"` + empty.Fingerprint + `"}`, "PATCH", 403},
		{"no-field", f.token, f.csrf, `{"expected_fingerprint":"` + empty.Fingerprint + `"}`, "PATCH", 400},
		{"both-value-and-clear", f.token, f.csrf, `{"field":"qq","value":"x","clear":true,"expected_fingerprint":"` + empty.Fingerprint + `"}`, "PATCH", 422},
		{"unknown-provider", f.token, f.csrf, `{"field":"weibo","value":"x","expected_fingerprint":"` + empty.Fingerprint + `"}`, "PATCH", 422},
		{"unknown-field", f.token, f.csrf, `{"field":"qq","nope":"x","expected_fingerprint":"` + empty.Fingerprint + `"}`, "PATCH", 400},
	}
	for _, tc := range cases {
		if w := oauthCredCall(f, tc.token, tc.csrf, tc.method, tc.body); w.Code != tc.want {
			t.Fatalf("%s: HTTP=%d want %d body=%s", tc.name, w.Code, tc.want, w.Body.String())
		}
	}
	var rows int
	if err := f.p.QueryRow(t.Context(), "SELECT count(*) FROM connect_login_credentials").Scan(&rows); err != nil || rows != 0 {
		t.Fatal("a rejected call still wrote a credential row", rows, err)
	}
}
