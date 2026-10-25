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

func TestReferralGross69FundsPreviewAndExactBasisConfirmation(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(p, false)
	s.FundsActionResolver = s.ResolveFundsAction
	user := identity.User{Role: "admin", Username: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	target := "/console/v1/admin/referral-policies/global/0"
	body := `{"expected":"0","enabled":true,"rate_bps":5000,"mode":"immediate","basis":"gross","refund_policy":"any_successful_refund_voids_reward"}`
	gross, summary, e := s.ResolveFundsAction(ctx, p, target, json.RawMessage(body))
	if e != nil || summary["basis"] != `"gross"` || !strings.Contains(summary["reward_basis"], "订单金额") || !strings.Contains(summary["reward_budget"], "原订单") {
		t.Fatal("gross preview missing bounded semantics", summary, e)
	}
	fee, _, e := s.ResolveFundsAction(ctx, p, target, json.RawMessage(strings.Replace(body, `"gross"`, `"fee"`, 1)))
	if e != nil || bytes.Equal(gross.Digest(), fee.Digest()) {
		t.Fatal("fee/gross same confirmation fingerprint", e)
	}
	if _, _, e = s.ResolveFundsAction(ctx, p, target, json.RawMessage(strings.Replace(body, `"gross"`, `"unproven"`, 1))); e == nil {
		t.Fatal("unproven basis accepted")
	}
	funds := identity.NewFundsAuthorization(p, nil)
	actor := identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.69"}
	if e = funds.SetPassword(ctx, actor, "contract-password-123", "", "local-gross-funds-456", ""); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"admin_funds_authorization_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	call := func(path, body, grant string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Origin", "http://localhost")
		r.RemoteAddr = "192.0.2.69:1234"
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		if grant != "" {
			r.Header.Set("X-Payment-Authorization", grant)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code, lookupData(t, w)
	}
	if code, _ := call(target, body, ""); code != 428 {
		t.Fatal("gross bypassed funds guard", code)
	}
	raw, _ := json.Marshal(map[string]any{"target": target, "body": json.RawMessage(body)})
	code, challenge := call("/console/v1/admin/security/funds-authorizations/challenges", string(raw), "")
	if code != 201 {
		t.Fatal("gross challenge rejected", code)
	}
	raw, _ = json.Marshal(map[string]any{"challenge_id": challenge["challenge_id"], "payment_password": "local-gross-funds-456"})
	code, grant := call("/console/v1/admin/security/funds-authorizations/confirm", string(raw), "")
	if code != 200 {
		t.Fatal("gross confirmation rejected", code)
	}
	authorization, _ := grant["authorization"].(string)
	if code, _ := call(target, strings.Replace(body, `"gross"`, `"fee"`, 1), authorization); code != 428 {
		t.Fatal("changed basis consumed grant", code)
	}
	code, saved := call(target, body, authorization)
	if code != 200 || saved["basis"] != "gross" || saved["revision"] != "1" {
		t.Fatal("exact gross policy rejected", code, saved)
	}
	if code, _ := call(target, body, authorization); code != 428 && code != 409 {
		t.Fatal("gross grant reused", code)
	}
	var versions, consumed, journals int
	if e = p.QueryRow(ctx, `SELECT (SELECT count(*) FROM referral_policies),(SELECT count(*) FROM admin_funds_grants WHERE action_kind='admin.referral_policy' AND consumed_at IS NOT NULL),(SELECT count(*) FROM journals)`).Scan(&versions, &consumed, &journals); e != nil || versions != 1 || consumed != 1 || journals != 0 {
		t.Fatal("policy grant not atomic or policy wrote money", versions, consumed, journals, e)
	}
}
