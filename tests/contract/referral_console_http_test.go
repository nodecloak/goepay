package contract_test

import (
	"context"
	"encoding/json"
	"goepay/internal/enrollment"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReferralConsoleActualHTTPRegistrationTokenPolicyFundsAndOwner(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(p, false)
	s.FundsActionResolver = s.ResolveFundsAction
	merchant, merchantCSRF, e := s.Sessions.Create(identity.User{Role: "merchant", Username: "merchant", ID: 1, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	adminUser := identity.User{Role: "admin", Username: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	admin, adminCSRF, e := s.Sessions.Create(adminUser)
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path, role, cookie, csrf, body, grant string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-CSRF-Token", csrf)
		r.RemoteAddr = "192.0.2.59:1234"
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: cookie})
		}
		if grant != "" {
			r.Header.Set("X-Payment-Authorization", grant)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("referral response cacheable")
		}
		var v map[string]any
		if json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal("invalid referral envelope")
		}
		return w.Code, v
	}
	get := func(path string) map[string]any {
		t.Helper()
		code, v := call("GET", "/console/v1/merchant/referral/"+path, "merchant", merchant, "", "", "")
		if code != 200 {
			t.Fatal("referral read status", code)
		}
		return v["data"].(map[string]any)
	}
	initial := get("token")
	if initial["version"] != "0" || initial["enabled"] != false {
		t.Fatal("initial token not explicit disabled")
	}
	if code, _ := call("POST", "/console/v1/merchant/referral/token/rotate", "merchant", merchant, "", `{"expected":"0"}`, ""); code != 403 {
		t.Fatal("missing token CSRF accepted", code)
	}
	code, issued := call("POST", "/console/v1/merchant/referral/token/rotate", "merchant", merchant, merchantCSRF, `{"expected":"0"}`, "")
	if code != 200 {
		t.Fatal("token issue", code)
	}
	issuedData := issued["data"].(map[string]any)
	bearer, ok := issuedData["token"].(string)
	if !ok || !strings.HasPrefix(bearer, "rp_") || issuedData["expires_at"] == nil {
		t.Fatal("missing one-time bearer/expiry")
	}
	masked := get("token")
	if _, ok := masked["token"]; ok {
		t.Fatal("existing bearer revealed")
	}
	if code, _ := call("POST", "/console/v1/merchant/referral/token/rotate", "merchant", merchant, merchantCSRF, `{"expected":"0"}`, ""); code != 409 {
		t.Fatal("lost-response CAS silently rotated", code)
	}
	for _, path := range []string{"overview?merchant_id=2", "token?merchant_id=2", "invitees?after_id=00", "rewards?limit=01", "invitees?after_id=0&after_id=0"} {
		if code, _ := call("GET", "/console/v1/merchant/referral/"+path, "merchant", merchant, "", "", ""); code != 400 {
			t.Fatal("owner/canonical list selector accepted", code)
		}
	}
	if code, _ := call("GET", "/console/v1/admin/referral/overview", "admin", admin, "", "", ""); code != 403 {
		t.Fatal("admin impersonated promoter", code)
	}
	var mailCode string
	svc, e := enrollment.New(p, s.Store, make([]byte, 32), enrollment.SenderFunc(func(_ context.Context, _ string, _ string, code string) error { mailCode = code; return nil }))
	if e != nil {
		t.Fatal(e)
	}
	s.Enrollment = enrollment.NewHandler(svc, "http://localhost")
	if _, e = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"registration_open","value":"true"}'),('settings','{"key":"registration_invite_required","value":"true"}'),('invitation-codes','{"code":"ADMISSION-ONLY","enabled":true,"max_uses":1}')`); e != nil {
		t.Fatal(e)
	}
	code, ch := call("POST", "/console/v1/public/verification-code", "", "", "", `{"purpose":"register","email":"referral-console@example.test"}`, "")
	if code != 202 {
		t.Fatal("OTP challenge", code)
	}
	registration := map[string]any{"challenge_id": ch["data"].(map[string]any)["challenge_id"], "code": mailCode, "email": "referral-console@example.test", "username": "referral-console", "name": "local child", "password": "local-register-password-456", "invite_code": "ADMISSION-ONLY", "promotion_token": bearer}
	raw, _ := json.Marshal(registration)
	code, registered := call("POST", "/console/v1/public/register", "", "", "", string(raw), "")
	if code != 201 {
		t.Fatal("explicit promotion plus admission registration", code)
	}
	child := registered["data"].(map[string]any)["merchant_id"]
	if data := get("overview"); data["invited_count"] != "1" || data["issued_amount"] != "0" || data["recovery_pending_amount"] != "0" {
		t.Fatal("relation confused with awarded wallet funds")
	}
	page := get("invitees?after_id=0&limit=1")
	rows := page["items"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["invited_id"] != child {
		t.Fatal("original registered relation absent")
	}
	safe, _ := json.Marshal(page)
	for _, private := range []string{bearer, "referral-console@example.test", "proof_hash", "challenge_id"} {
		if strings.Contains(string(safe), private) {
			t.Fatal("relation list leaked registration evidence")
		}
	}
	if code, v := call("GET", "/user/invite/inviteInfo", "merchant", merchant, "", "", ""); code != 200 || v["data"].(map[string]any)["invited_count"] != "1" {
		t.Fatal("exact safe info alias", code)
	}
	if code, _ := call("POST", "/user/invite/inviteList", "merchant", merchant, "", `{"after_id":"0","limit":20}`, ""); code != 403 {
		t.Fatal("legacy alias CSRF bypass")
	}
	if code, v := call("POST", "/user/invite/inviteList", "merchant", merchant, merchantCSRF, `{"after_id":"0","limit":20}`, ""); code != 200 || len(v["data"].(map[string]any)["items"].([]any)) != 1 {
		t.Fatal("safe list alias", code)
	}
	if code, _ := call("GET", "/user/invite/inviteList", "merchant", merchant, "", "", ""); code != 405 {
		t.Fatal("legacy alias verb expanded")
	}
	if code, _ := call("POST", "/console/v1/merchant/referral/token/revoke", "merchant", merchant, merchantCSRF, `{"expected":"1"}`, ""); code != 200 {
		t.Fatal("revoke", code)
	}
	if get("overview")["invited_count"] != "1" {
		t.Fatal("revoke rewrote bound relation")
	}
	if code, _ := call("POST", "/console/v1/admin/referral-policies/global/0", "admin", admin, adminCSRF, `{"expected":"0","enabled":true,"rate_bps":5000,"mode":"weekly","basis":"fee","refund_policy":"any_successful_refund_voids_reward"}`, ""); code != 422 {
		t.Fatal("unsupported weekly policy accepted", code)
	}
	funds := identity.NewFundsAuthorization(p, nil)
	actor := identity.FundsActor{User: adminUser, SessionToken: admin, ClientIP: "192.0.2.59"}
	if e = funds.SetPassword(ctx, actor, "contract-password-123", "", "local-referral-funds-456", ""); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"admin_funds_authorization_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	target := "/console/v1/admin/referral-policies/global/0"
	body := `{"expected":"0","enabled":true,"rate_bps":5000,"mode":"immediate","basis":"fee","refund_policy":"any_successful_refund_voids_reward"}`
	if code, _ := call("POST", target, "admin", admin, adminCSRF, body, ""); code != 428 {
		t.Fatal("policy funds guard missing", code)
	}
	challengeBody, _ := json.Marshal(map[string]any{"target": target, "body": json.RawMessage(body)})
	code, challenge := call("POST", "/console/v1/admin/security/funds-authorizations/challenges", "admin", admin, adminCSRF, string(challengeBody), "")
	if code != 201 {
		t.Fatal("policy challenge", code)
	}
	confirm, _ := json.Marshal(map[string]any{"challenge_id": challenge["data"].(map[string]any)["challenge_id"], "payment_password": "local-referral-funds-456"})
	code, grant := call("POST", "/console/v1/admin/security/funds-authorizations/confirm", "admin", admin, adminCSRF, string(confirm), "")
	if code != 200 {
		t.Fatal("policy funds confirmation", code)
	}
	authorization := grant["data"].(map[string]any)["authorization"].(string)
	if code, _ := call("POST", target, "admin", admin, adminCSRF, strings.Replace(body, "5000", "6000", 1), authorization); code != 428 {
		t.Fatal("policy amount switch accepted", code)
	}
	code, saved := call("POST", target, "admin", admin, adminCSRF, body, authorization)
	if code != 200 || saved["data"].(map[string]any)["revision"] != "1" {
		t.Fatal("authorized policy save", code)
	}
	var consumed int
	if e = p.QueryRow(ctx, "SELECT count(*) FROM admin_funds_grants WHERE action_kind='admin.referral_policy' AND resource_id=$1 AND consumed_at IS NOT NULL", target).Scan(&consumed); e != nil || consumed != 1 {
		t.Fatal("policy grant not consumed once", consumed, e)
	}
	if code, _ := call("POST", target, "admin", admin, adminCSRF, body, ""); code != 428 && code != 409 {
		t.Fatal("policy stale version silently saved", code)
	}
	var policies int
	if e = p.QueryRow(ctx, "SELECT count(*) FROM referral_policies").Scan(&policies); e != nil || policies != 1 {
		t.Fatal("unexpected policy versions", policies, e)
	}
}
