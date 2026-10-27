package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMerchantAccountProfileKeysAndPassword(t *testing.T) {
	s, pool, _ := fixture(t)
	s.Sessions = identity.NewPersistentSessions(pool, false)
	var version int64
	if e := pool.QueryRow(t.Context(), `SELECT auth_version FROM console_users WHERE username='merchant' AND role='merchant'`).Scan(&version); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", ID: 1, Name: "Merchant", Role: "merchant", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	request := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(method, "/console/v1/merchant/account/"+path, bytes.NewBufferString(body))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e, w.Body.String())
		}
		return w.Code, out
	}
	code, out := request("GET", "api-info", "")
	if code != 200 {
		t.Fatal(code, out)
	}
	raw, _ := json.Marshal(out)
	if bytes.Contains(raw, []byte("contract-secret")) || bytes.Contains(raw, []byte("PRIVATE KEY")) {
		t.Fatal("secret leaked")
	}
	code, out = request("PATCH", "profile", `{"name":"Updated","email":"user@example.com"}`)
	if code != 200 {
		t.Fatal(code, out)
	}
	_, out = request("GET", "profile", "")
	data := out["data"].(map[string]any)
	if data["name"] != "Updated" || data["email"] != "user@example.com" {
		t.Fatal(out)
	}
	code, _ = request("POST", "api-key/rotate", `{"current_password":"wrong"}`)
	if code != 422 {
		t.Fatal("wrong current password must remain retryable in the same valid session", code)
	}
	if merchant, e := s.Store.Merchant(t.Context(), 1); e != nil || merchant.APIKey != "contract-secret" {
		t.Fatal("wrong current password changed the key", e)
	}
	if _, ok := s.Sessions.Get(token, "merchant"); !ok {
		t.Fatal("wrong current password revoked the original valid session")
	}
	code, out = request("POST", "api-key/rotate", `{"current_password":"contract-password-123"}`)
	if code != 200 {
		t.Fatal(code, out)
	}
	newKey := out["data"].(map[string]any)["api_key"].(string)
	merchant, e := s.Store.Merchant(context.Background(), 1)
	if e != nil || merchant.APIKey != newKey || newKey == "contract-secret" {
		t.Fatal("key not persisted")
	}
	_, out = request("GET", "api-info", "")
	raw, _ = json.Marshal(out)
	if strings.Contains(string(raw), newKey) {
		t.Fatal("rotated key revealed again")
	}
	code, out = request("POST", "password", `{"current_password":"contract-password-123","new_password":"new-contract-password-123"}`)
	if code != 200 {
		t.Fatal(code, out)
	}
	code, _ = request("GET", "profile", "")
	if code != 401 {
		t.Fatal("password change kept old session")
	}
}
func TestAdminPasswordChangeRevokesSession(t *testing.T) {
	s, pool, _ := fixture(t)
	s.Sessions = identity.NewPersistentSessions(pool, false)
	var version int64
	if e := pool.QueryRow(t.Context(), `SELECT auth_version FROM console_users WHERE username='admin' AND role='admin'`).Scan(&version); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "admin", ID: 1, Name: "Admin", Role: "admin", AdminClass: "super", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/console/v1/admin/account/password", strings.NewReader(`{"current_password":"contract-password-123","new_password":"new-admin-password-123"}`))
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, ok := s.Sessions.Get(token, "admin"); ok {
		t.Fatal("admin oldsession survived")
	}
}
