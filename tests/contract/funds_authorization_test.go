package contract_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
)

func TestFundsAuthorizationHTTPStrictJSONCSRFAndSessionBoundGrant(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(p, false)
	user := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	resolverCalls := 0
	s.FundsActionResolver = func(_ context.Context, _ identity.FundsQuerier, target string, body json.RawMessage) (identity.FundsAction, map[string]string, error) {
		resolverCalls++
		if target != "/console/v1/admin/transfers" {
			return identity.FundsAction{}, nil, identity.ErrFundsInvalid
		}
		sum := sha256.Sum256(body)
		a, e := identity.NewFundsAction("transfer.create", "fixed-external-no", map[string]string{"body_hmac": hex.EncodeToString(sum[:]), "snapshot_hmac": "trusted-read-only-snapshot"})
		return a, map[string]string{"merchant_id": "1", "amount": "1.25", "recipient": "***test"}, e
	}
	request := func(method, path, body, csrfHeader string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrfHeader)
		r.RemoteAddr = "[::ffff:192.0.2.1]:1234"
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Error("cacheable funds response")
		}
		return w
	}
	status := request("GET", "/console/v1/admin/security/payment-password", "", "")
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"configured":false`) {
		t.Fatal(status.Code, status.Body.String())
	}
	good := `{"current_password":"contract-password-123","current_payment_password":"","new_payment_password":"separate-admin-funds-456","code":""}`
	if w := request("PUT", "/console/v1/admin/security/payment-password", good, ""); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	for _, raw := range []string{`null`, `[]`, good + ` {}`, strings.Replace(good, `"code":""`, `"code":"","code":""`, 1), strings.Replace(good, `"code":""`, `"unexpected":1`, 1)} {
		if w := request("PUT", "/console/v1/admin/security/payment-password", raw, csrf); w.Code != 400 {
			t.Fatal("ambiguous body", raw, w.Code, w.Body.String())
		}
	}
	if w := request("PUT", "/console/v1/admin/security/payment-password", good, csrf); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	wrong := `{"current_password":"wrong-login","current_payment_password":"separate-admin-funds-456","new_payment_password":"different-admin-funds-789","code":""}`
	if w := request("PUT", "/console/v1/admin/security/payment-password", wrong, csrf); w.Code != 403 || !strings.Contains(w.Body.String(), "FUNDS_AUTHENTICATION_FAILED") {
		t.Fatal("verification must not log out a valid session", w.Code, w.Body.String())
	}
	if w := request("GET", "/console/v1/admin/security/payment-password", "", ""); w.Code != 200 {
		t.Fatal("verification failure invalidated administrator session", w.Code)
	}
	preview := `{"target":"/console/v1/admin/transfers","body":{"external_no":"fixed-external-no","amount":"1.25","recipient":"synthetic-test"}}`
	duplicate := `{"target":"/console/v1/admin/transfers","body":{"amount":"1.25","amount":"999.00"}}`
	if w := request("POST", "/console/v1/admin/security/funds-authorizations/challenges", duplicate, csrf); w.Code != 400 || resolverCalls != 0 {
		t.Fatal("duplicate reached action builder", w.Code, resolverCalls)
	}
	w := request("POST", "/console/v1/admin/security/funds-authorizations/challenges", preview, csrf)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var envelope struct {
		Data struct {
			Challenge string `json:"challenge_id"`
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &envelope); e != nil {
		t.Fatal(e)
	}
	confirm := `{"challenge_id":"` + envelope.Data.Challenge + `","payment_password":"separate-admin-funds-456"}`
	w = request("POST", "/console/v1/admin/security/funds-authorizations/confirm", confirm, csrf)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var grant struct{ Data identity.FundsGrant }
	json.Unmarshal(w.Body.Bytes(), &grant)
	if len(grant.Data.Authorization) != 43 {
		t.Fatal("missing grant")
	}
	var stored string
	if e = p.QueryRow(ctx, "SELECT encode(grant_hash,'hex') FROM admin_funds_grants").Scan(&stored); e != nil || stored == grant.Data.Authorization {
		t.Fatal("raw grant persisted", e)
	}
	// A challenge cannot be confirmed twice, even in the same session.
	if w = request("POST", "/console/v1/admin/security/funds-authorizations/confirm", confirm, csrf); w.Code != 428 {
		t.Fatal("challenge replay", w.Code, w.Body.String())
	}
	// A different original session cannot inherit the first one's authorization.
	token, csrf, e = s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	if w = request("POST", "/console/v1/admin/security/funds-authorizations/confirm", confirm, csrf); w.Code != 428 {
		t.Fatal("session transfer", w.Code)
	}
	var attempts, grants int
	if e = p.QueryRow(ctx, "SELECT (SELECT count(*) FROM provider_attempts),(SELECT count(*) FROM admin_funds_grants)").Scan(&attempts, &grants); e != nil || attempts != 0 || grants != 1 {
		t.Fatal("security action sent payment", attempts, grants, e)
	}
}
