package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/proxyconfig"
)

func proxyCredCall(f channel168Fixture, token, csrf, method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/settings/proxy-credentials", strings.NewReader(body))
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

func proxyCredView(t *testing.T, w *httptest.ResponseRecorder) proxyconfig.View {
	t.Helper()
	var out struct {
		Code int              `json:"code"`
		Data proxyconfig.View `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("proxy credential call failed: HTTP=%d %s", w.Code, w.Body.String())
	}
	return out.Data
}

func TestProxyCredentialsStoreWriteOnlyAndCAS(t *testing.T) {
	f := channel168Local(t)
	// Empty store: neither secret set, a stable fingerprint, no plaintext.
	empty := proxyCredView(t, proxyCredCall(f, f.token, f.csrf, "GET", ""))
	if empty.PasswordSet || empty.APIKeySet || len(empty.Fingerprint) != 64 {
		t.Fatalf("empty view wrong: %+v", empty)
	}
	// Set both secrets.
	set := proxyCredView(t, proxyCredCall(f, f.token, f.csrf, "PATCH", `{"password":"relay-secret-pwd","apikey":"AbCdEf2345xyzKMNPQ78","expected_fingerprint":"`+empty.Fingerprint+`"}`))
	if !set.PasswordSet || !set.APIKeySet || set.Fingerprint == empty.Fingerprint {
		t.Fatalf("set view wrong: %+v", set)
	}
	// The secrets are never echoed anywhere.
	for _, w := range []*httptest.ResponseRecorder{proxyCredCall(f, f.token, f.csrf, "GET", "")} {
		if strings.Contains(w.Body.String(), "relay-secret-pwd") || strings.Contains(w.Body.String(), "AbCdEf2345xyzKMNPQ78") {
			t.Fatal("secret leaked through the console")
		}
	}
	// A stale fingerprint is refused (CAS).
	if w := proxyCredCall(f, f.token, f.csrf, "PATCH", `{"clear_password":true,"expected_fingerprint":"`+empty.Fingerprint+`"}`); w.Code != 409 {
		t.Fatal("stale fingerprint accepted", w.Code)
	}
	// Clear only the password; the API key stays.
	cleared := proxyCredView(t, proxyCredCall(f, f.token, f.csrf, "PATCH", `{"clear_password":true,"expected_fingerprint":"`+set.Fingerprint+`"}`))
	if cleared.PasswordSet || !cleared.APIKeySet {
		t.Fatalf("selective clear wrong: %+v", cleared)
	}
	// The server-internal Resolve returns the decrypted API key, no actor.
	store, err := f.s.proxyCredentialStore()
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := store.Resolve(t.Context())
	if err != nil || secrets.Password != "" || secrets.APIKey != "AbCdEf2345xyzKMNPQ78" {
		t.Fatalf("resolve wrong: %+v %v", secrets, err)
	}
	// The ciphertext on disk is not the plaintext.
	var pwd, apikey []byte
	if err = f.p.QueryRow(t.Context(), "SELECT pwd_cipher,apikey_cipher FROM proxy_credentials WHERE id=true").Scan(&pwd, &apikey); err != nil {
		t.Fatal(err)
	}
	if len(pwd) != 0 || strings.Contains(string(apikey), "AbCdEf2345xyzKMNPQ78") {
		t.Fatal("secret stored in plaintext or cleared value lingers")
	}
}

func TestProxyCredentialsRequireSuperAdminAndCSRF(t *testing.T) {
	f := channel168Local(t)
	if _, err := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('proxyop','admin',171,'Proxy operator','unused-synthetic','operator')`); err != nil {
		t.Fatal(err)
	}
	opToken, opCSRF, err := f.s.Sessions.Create(identity.User{ID: 171, Role: "admin", Username: "proxyop", AdminClass: "operator", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	empty := proxyCredView(t, proxyCredCall(f, f.token, f.csrf, "GET", ""))
	cases := []struct {
		name, token, csrf, body string
		method                  string
		want                    int
	}{
		{"operator-read", opToken, opCSRF, "", "GET", 403},
		{"operator-write", opToken, opCSRF, `{"password":"x","expected_fingerprint":"` + empty.Fingerprint + `"}`, "PATCH", 403},
		{"no-csrf", f.token, "", `{"password":"x","expected_fingerprint":"` + empty.Fingerprint + `"}`, "PATCH", 403},
		{"no-field", f.token, f.csrf, `{"expected_fingerprint":"` + empty.Fingerprint + `"}`, "PATCH", 422},
		{"both-pwd-and-clear", f.token, f.csrf, `{"password":"x","clear_password":true,"expected_fingerprint":"` + empty.Fingerprint + `"}`, "PATCH", 422},
		{"unknown-field", f.token, f.csrf, `{"nope":"x","expected_fingerprint":"` + empty.Fingerprint + `"}`, "PATCH", 400},
	}
	for _, tc := range cases {
		if w := proxyCredCall(f, tc.token, tc.csrf, tc.method, tc.body); w.Code != tc.want {
			t.Fatalf("%s: HTTP=%d want %d body=%s", tc.name, w.Code, tc.want, w.Body.String())
		}
	}
	var rows int
	if err := f.p.QueryRow(t.Context(), "SELECT count(*) FROM proxy_credentials").Scan(&rows); err != nil || rows != 0 {
		t.Fatal("a rejected call still wrote a credential row", rows, err)
	}
}
