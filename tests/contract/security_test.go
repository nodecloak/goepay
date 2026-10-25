package contract_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTOTPSetupRequiresPasswordAndCannotSilentlyDisable(t *testing.T) {
	s, pool, _ := fixture(t)
	var err error
	s.MFA, err = identity.NewMFA(pool, bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	token, csrf, _ := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1})
	call := func(method, path, body string) (int, map[string]any) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	code, _ := call("POST", "/console/v1/admin/security/totp/prepare", `{"current_password":"wrong"}`)
	if code != 401 {
		t.Fatalf("wrongpassword %d", code)
	}
	code, out := call("POST", "/console/v1/admin/security/totp/prepare", `{"current_password":"contract-password-123"}`)
	if code != 200 {
		t.Fatalf("prepare %d %v", code, out)
	}
	secret := out["data"].(map[string]any)["secret"].(string)
	if len(secret) != 32 {
		t.Fatalf("invalidsecret length %d", len(secret))
	}
	var stored []byte
	if err = pool.QueryRow(context.Background(), "SELECT pending_secret FROM console_mfa WHERE username='admin' AND role='admin'").Scan(&stored); err != nil || bytes.Contains(stored, []byte(secret)) {
		t.Fatal("secret storage")
	}
	code, _ = call("POST", "/console/v1/admin/security/totp/confirm", `{"code":"123"}`)
	if code != 401 {
		t.Fatalf("invalidcode %d", code)
	}
	code, out = call("GET", "/console/v1/admin/security/totp", "")
	if code != 200 || out["data"].(map[string]any)["enabled"] != false {
		t.Fatal(out)
	}
	makeCode := func(counter int64) string {
		raw, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(counter))
		h := hmac.New(sha1.New, raw)
		h.Write(buf[:])
		digest := h.Sum(nil)
		offset := digest[19] & 15
		return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(digest[offset:offset+4])&0x7fffffff)%1000000)
	}
	counter := time.Now().Unix() / 30
	code, out = call("POST", "/console/v1/admin/security/totp/confirm", fmt.Sprintf(`{"code":%q}`, makeCode(counter)))
	if code != 200 {
		t.Fatalf("confirm %d %v", code, out)
	}
	code, _ = call("GET", "/console/v1/admin/session", "")
	if code != 401 {
		t.Fatalf("MFA change did not revoke session %d", code)
	}
	login := func(code string) int {
		r := httptest.NewRequest("POST", "/console/v1/admin/login", strings.NewReader(fmt.Sprintf(`{"username":"admin","password":"contract-password-123","code":%q}`, code)))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if code := login(""); code != 401 {
		t.Fatalf("missingMFA %d", code)
	}
	saved := s.MFA
	s.MFA = nil
	if code := login(makeCode(counter + 1)); code != 401 {
		t.Fatalf("missingMFAkey bypass %d", code)
	}
	s.MFA = saved
	if code := login(makeCode(counter + 1)); code != 200 {
		t.Fatalf("validMFA %d", code)
	}
	if code := login(makeCode(counter + 1)); code != 401 {
		t.Fatalf("replayedMFA %d", code)
	}
}
