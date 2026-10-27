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
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/identity"
)

func merchant174KeyLogin(f signatureFixture163, mid, key, code string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]string{"merchant_id": mid, "api_key": key, "code": code})
	r := httptest.NewRequest("POST", "/console/v1/merchant/key-login", strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func merchant174TOTP(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	raw, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if e != nil {
		t.Fatal(e)
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(at.Unix()/30))
	m := hmac.New(sha1.New, raw)
	m.Write(b[:])
	v := m.Sum(nil)
	i := v[len(v)-1] & 15
	n := binary.BigEndian.Uint32(v[i:i+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000)
}
func merchant174EnrollMFA(t *testing.T, f signatureFixture163) (string, signatureFixture163) {
	t.Helper()
	ctx := context.Background()
	var e error
	f.s.MFA, e = identity.NewMFA(f.p, bytes.Repeat([]byte{3}, 32))
	if e != nil {
		t.Fatal(e)
	}
	enrollment, e := f.s.MFA.Prepare(ctx, "merchant", "merchant")
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.MFA.Confirm(ctx, "merchant", "merchant", merchant174TOTP(t, enrollment.Secret, time.Now().Add(-30*time.Second))); e != nil {
		t.Fatal("synthetic prior-step MFA enrollment failed", e)
	}
	var version int64
	if e = f.p.QueryRow(ctx, `SELECT auth_version FROM console_users WHERE username='merchant' AND role='merchant'`).Scan(&version); e != nil {
		t.Fatal(e)
	}
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: 2, Role: "merchant", Username: "merchant", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	return enrollment.Secret, f
}
func TestMerchant174KeyLoginHonorsExistingTOTPAndRejectsReplay(t *testing.T) {
	f := signatureFixture(t, 2)
	secret, f := merchant174EnrollMFA(t, f)
	ctx := context.Background()
	m, e := f.s.Store.Merchant(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	for _, code := range []string{"", "not-a-code"} {
		if w := merchant174KeyLogin(f, "2", m.APIKey, code); w.Code != 401 || len(w.Result().Cookies()) != 0 {
			t.Fatal("missing/invalid MFA issued key session", w.Code)
		}
	}
	code := merchant174TOTP(t, secret, time.Now())
	w := merchant174KeyLogin(f, "2", m.APIKey, code)
	if w.Code != 200 {
		t.Fatal("valid key+MFA rejected", w.Code)
	}
	if w = merchant174KeyLogin(f, "2", m.APIKey, code); w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatal("MFA replay issued another key session", w.Code)
	}
}
func TestMerchant174KeyLoginCloseDoesNotDisablePasswordOrExistingOwnerSession(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	m, e := f.s.Store.Merchant(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	merchant174Set(t, f, 0, 1, 0)
	if w := merchant174KeyLogin(f, "2", m.APIKey, ""); w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatal("closed key login issued session", w.Code)
	}
	apiSwitchState174(t, f.request("GET", "api-switches", "", f.csrf))
	r := httptest.NewRequest("POST", "/console/v1/merchant/login", strings.NewReader(`{"username":"merchant","password":"contract-password-123","code":""}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatal("personal key-login close disabled password authentication", w.Code)
	}
}
func TestMerchant174KeyLoginStrictBodyURLAndPersistentIdentityDoNotExposeSecrets(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	m, e := f.s.Store.Merchant(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	valid := `{"merchant_id":"2","api_key":"synthetic-bad-key-marker174","code":""}`
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/console/v1/merchant/key-login?api_key=synthetic-url-marker174", "", 405},
		{"POST", "/console/v1/merchant/key-login?api_key=synthetic-url-marker174", valid, 400},
		{"POST", "/console/v1/merchant/key-login", strings.Replace(valid, `"2"`, `2`, 1), 400},
		{"POST", "/console/v1/merchant/key-login", strings.Replace(valid, `"2"`, `"02"`, 1), 400},
		{"POST", "/console/v1/merchant/key-login", strings.TrimSuffix(valid, "}") + `,"api_key":"duplicate"}`, 400},
		{"POST", "/console/v1/merchant/key-login", strings.TrimSuffix(valid, "}") + `,"username":"admin"}`, 400},
		{"POST", "/console/v1/merchant/key-login", strings.Replace(valid, `"synthetic-bad-key-marker174"`, `null`, 1), 400},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != tc.want || len(w.Result().Cookies()) != 0 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "marker174") {
			t.Fatal("strict key login body/url/status/secret", w.Code, tc.want)
		}
	}
	for _, sql := range []string{`UPDATE console_users SET enabled=false WHERE username='merchant'`, `UPDATE console_users SET enabled=true WHERE username='merchant';UPDATE merchants SET enabled=false WHERE id=2`, `UPDATE merchants SET enabled=true WHERE id=2;DELETE FROM console_sessions WHERE username='merchant';DELETE FROM console_users WHERE username='merchant'`} {
		if _, e = f.p.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
		if w := merchant174KeyLogin(f, "2", m.APIKey, ""); w.Code != 401 || len(w.Result().Cookies()) != 0 {
			t.Fatal("absent/disabled persistent identity issued session", w.Code)
		}
	}
	var leaked bool
	if e = f.p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM console_login_audit WHERE username LIKE '%marker174%' OR username=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE changes::text LIKE '%marker174%' OR changes::text LIKE '%'||$1||'%')`, m.APIKey).Scan(&leaked); e != nil || leaked {
		t.Fatal("key/URL/body leaked into login or backoffice audit", e)
	}
}
