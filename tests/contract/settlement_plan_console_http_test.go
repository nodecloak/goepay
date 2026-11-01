package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestSettlementPlanConsoleMandatoryGrantEvenWhenGlobalOff(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(p, false)
	s.FundsActionResolver = s.ResolveFundsAction
	adminUser := identity.User{Role: "admin", Username: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	admin, adminCSRF, err := s.Sessions.Create(adminUser)
	if err != nil {
		t.Fatal(err)
	}
	merchant, merchantCSRF, err := s.Sessions.Create(identity.User{Role: "merchant", Username: "merchant", ID: 1, AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, role, body, grant string) (int, map[string]any) {
		t.Helper()
		cookie, csrf := admin, adminCSRF
		if role == "merchant" {
			cookie, csrf = merchant, merchantCSRF
		}
		r := httptest.NewRequest(method, "/console/v1/"+role+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Origin", "http://localhost")
		r.RemoteAddr = "192.0.2.60:1234"
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: cookie})
		if grant != "" {
			r.Header.Set("X-Payment-Authorization", grant)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("plan response is cacheable")
		}
		var v map[string]any
		if json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal("invalid response envelope")
		}
		return w.Code, v
	}
	zero := int64(0)
	recipient, err := s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "alipay", Account: "local-plan-payee@example.test", Name: "Local Plan Payee"}, &zero, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	prefBody := fmt.Sprintf(`{"expected_revision":"0","enabled":true,"recipient_config_id":"%d"}`, recipient.ConfigID)
	if code, _ := call("PUT", "/account/settlement-schedule", "merchant", prefBody, ""); code != 200 {
		t.Fatal("owner opt-in", code)
	}
	if code, _ := call("GET", "/account/settlement-schedule?merchant_id=2", "merchant", "", ""); code != 400 {
		t.Fatal("preference owner override", code)
	}
	if code, _ := call("GET", "/settlement-plans", "merchant", "", ""); code != 403 {
		t.Fatal("merchant reached plan administration", code)
	}
	var now time.Time
	if err = p.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	definition := map[string]any{"name": "Local pending requests only", "mode": "generate_only", "merchant_ids": []string{"1"}, "schedule": map[string]any{"frequency": "hour", "interval": "1", "anchor_at": now.UTC().Truncate(time.Hour).Format(time.RFC3339), "execute_hour": 0, "execute_minute": 0, "execute_second": 0}}
	raw, _ := json.Marshal(definition)
	code, created := call("POST", "/settlement-plans", "admin", string(raw), "")
	if code != 201 {
		t.Fatal("create disabled definition", code)
	}
	plan := created["data"].(map[string]any)
	id := plan["id"].(string)
	if plan["enabled"] != false || plan["revision"] != "1" || plan["last_authorization_id"] != "0" {
		t.Fatal("definition created implicit financial authority")
	}
	if enforced, e := identity.FundsEnforcedTx(ctx, p); e != nil || enforced {
		t.Fatal("fixture must prove global optional confirmation is disabled", e)
	}
	input := map[string]any{"expected_revision": "1", "expected_authorization_id": "0", "valid_from": now.UTC().Truncate(time.Second).Format(time.RFC3339), "expires_at": now.Add(time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339), "max_item_gross": "10000", "max_run_gross": "10000", "max_day_gross": "10000", "max_day_items": "1"}
	body, _ := json.Marshal(input)
	target := "/settlement-plans/" + id + "/authorizations"
	if code, _ := call("POST", target, "admin", string(body), ""); code != 428 {
		t.Fatal("global-off plan bypassed mandatory grant", code)
	}
	funds := identity.NewFundsAuthorization(p, nil)
	if err = funds.SetPassword(ctx, identity.FundsActor{User: adminUser, SessionToken: admin, ClientIP: "192.0.2.60"}, "contract-password-123", "", "local-plan-funds-password-456", ""); err != nil {
		t.Fatal(err)
	}
	challengeBody, _ := json.Marshal(map[string]any{"target": "/console/v1/admin" + target, "body": json.RawMessage(body)})
	code, challenge := call("POST", "/security/funds-authorizations/challenges", "admin", string(challengeBody), "")
	if code != 201 {
		t.Fatal("mandatory plan challenge", code)
	}
	summary := challenge["data"].(map[string]any)["summary"].(map[string]any)
	if summary["merchant_ids"] != "1" || summary["retained_money"] != "0.00" || summary["maturity"] != "D+0" || !strings.Contains(fmt.Sprint(summary["recipients"]), "alipay") || strings.Contains(fmt.Sprint(summary), "local-plan-payee@example.test") {
		t.Fatal("authorization lacks concrete masked scope and financial policy", summary)
	}
	if summary["group_maturity_rules"] != "商户1：D+0" || summary["group_fee_rules"] != "商户1：0.00%" || summary["fee_rate_bps"] != "0" || summary["group_minimum_rules"] != "商户1："+fmt.Sprint(summary["minimum_money"])+"元" || !strings.Contains(fmt.Sprint(summary["fee_scope"]), "逐商户") {
		t.Fatal("authorization summary lost its per-merchant effective financial scope", summary)
	}
	confirm, _ := json.Marshal(map[string]any{"challenge_id": challenge["data"].(map[string]any)["challenge_id"], "payment_password": "local-plan-funds-password-456"})
	code, grant := call("POST", "/security/funds-authorizations/confirm", "admin", string(confirm), "")
	if code != 200 {
		t.Fatal("mandatory plan confirmation", code)
	}
	authorization := grant["data"].(map[string]any)["authorization"].(string)
	input["max_item_gross"] = "9999"
	changed, _ := json.Marshal(input)
	if code, _ := call("POST", target, "admin", string(changed), authorization); code != 428 {
		t.Fatal("changed persistent budget adopted original grant", code)
	}
	code, authorized := call("POST", target, "admin", string(body), authorization)
	if code != 201 {
		t.Fatal("bounded plan authorization", code)
	}
	authID := authorized["data"].(map[string]any)["id"].(string)
	var consumed int
	if err = p.QueryRow(ctx, "SELECT count(*) FROM admin_funds_grants WHERE action_kind='admin.settlement_plan_authorize' AND consumed_at IS NOT NULL").Scan(&consumed); err != nil || consumed != 1 {
		t.Fatal("mandatory grant not consumed once", consumed, err)
	}
	if code, _ := call("POST", target, "admin", string(body), authorization); code != 409 && code != 428 {
		t.Fatal("authority replay silently replaced original", code)
	}
	if code, _ := call("GET", "/settlement-plans?after_id=00", "admin", "", ""); code != 400 {
		t.Fatal("noncanonical list cursor", code)
	}
	if code, _ := call("POST", "/settlement-plan-authorizations/"+authID+"/revoke", "admin", `{}`, ""); code != 200 {
		t.Fatal("emergency revoke requires unnecessary grant or failed", code)
	}
	code, stopped := call("GET", "/settlement-plans/"+id, "admin", "", "")
	if code != 200 || stopped["data"].(map[string]any)["enabled"] != false {
		t.Fatal("revoke did not stop new generation", code)
	}
	var journals int
	if err = p.QueryRow(ctx, "SELECT count(*) FROM journals").Scan(&journals); err != nil || journals != 0 {
		t.Fatal("definitions/authorizations caused money movement", journals, err)
	}
}
