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

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

type autoHTTPProvider73 struct {
	settlementHTTPProvider
	account string
}

func (p *autoHTTPProvider73) TransferAccountIdentity() string { return p.account }
func (p *autoHTTPProvider73) TransferProductIdentity() string {
	return "alipay.fund.trans.toaccount.transfer"
}

func TestSettlementAuto73HTTPIndependentMandatoryGrantStrictScopeAndRevocation(t *testing.T) {
	s, p, key := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(p, false)
	s.FundsActionResolver = s.ResolveFundsAction
	user := identity.User{Role: "admin", Username: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	merchant, mcsrf, e := s.Sessions.Create(identity.User{Role: "merchant", Username: "merchant", ID: 1, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "settlement-http-app", SellerID: "2088000000000073", PrivateKey: key, PublicKey: &key.PublicKey})
	if e != nil {
		t.Fatal(e)
	}
	provider := &autoHTTPProvider73{settlementHTTPProvider: settlementHTTPProvider{Provider: ali}, account: "seller:2088000000000073"}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, AllowLive: true, Providers: map[int64]adapter.Provider{7: provider}, MerchantChannels: map[int64][]int64{1: {7}}}
	s.MerchantChannels = s.ProviderFlow.MerchantChannels
	if enforced, e := identity.FundsEnforcedTx(ctx, p); e != nil || enforced {
		t.Fatal("must test global confirmation off", e)
	}
	zero := int64(0)
	r, e := s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "alipay", Account: "auto73-private-payee@example.invalid", Name: "Auto73 Private Name"}, &zero, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	actor := payment.SettlementPlanActor{Role: "admin", Class: "super", ID: 1, Name: "admin"}
	if _, e = s.Store.SetSettlementSchedulePreference(ctx, 1, 0, true, r.ConfigID, payment.SettlementPlanActor{Role: "merchant", ID: 1, Name: "merchant"}); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e = p.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		t.Fatal(e)
	}
	plan, e := s.Store.CreateSettlementPlan(ctx, payment.SettlementPlanInput{Name: "local automatic policy", Mode: "generate_only", MerchantIDs: []int64{1}, Schedule: payment.SettlementScheduleSpec{Frequency: "hour", Interval: 1, AnchorAt: now.Truncate(time.Hour)}}, actor)
	if e != nil {
		t.Fatal(e)
	}
	guard := payment.WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return nil })
	gen, e := s.Store.AuthorizeSettlementPlan(guard, plan.ID, plan.Revision, payment.SettlementPlanAuthorizationInput{ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 1}, actor)
	if e != nil {
		t.Fatal(e)
	}
	request := func(method, path, body, grant, cookie, csrfHeader, role string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, "/console/v1/"+role+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfHeader)
		req.Header.Set("Origin", "http://localhost")
		req.Header.Set("X-Payment-Authorization", grant)
		req.RemoteAddr = "192.0.2.73:1234"
		req.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: cookie})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("automatic authority response cacheable")
		}
		var out map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return w.Code, out
	}
	call := func(method, path, body, grant string) (int, map[string]any) {
		return request(method, path, body, grant, token, csrf, "admin")
	}
	base := fmt.Sprintf("/settlement-plans/%d", plan.ID)
	target := base + "/auto-authorizations"
	input := map[string]any{"expected_revision": fmt.Sprint(plan.Revision), "expected_authorization_id": "0", "generation_authorization_id": fmt.Sprint(gen.ID), "valid_from": gen.ValidFrom, "expires_at": gen.ExpiresAt, "max_item_gross": "1000", "max_run_gross": "1000", "max_day_gross": "1000", "max_day_items": "1", "cost_policy": "provider_managed_excluded", "channels": []map[string]string{{"merchant_id": "1", "channel_id": "7"}}}
	raw, _ := json.Marshal(input)
	body := string(raw)
	if code, _ := call("GET", base+"/auto-authorization", "", ""); code != 200 {
		t.Fatal("read absent independent authority", code)
	}
	if code, _ := call("POST", target, body, ""); code != 428 {
		t.Fatal("global-off bypassed independent funds confirmation", code)
	}
	if code, _ := request("POST", target, body, "", token, "", "admin"); code != 403 {
		t.Fatal("automatic authority CSRF", code)
	}
	if code, _ := request("GET", base+"/auto-authorization", "", "", merchant, mcsrf, "merchant"); code != 403 {
		t.Fatal("merchant read admin automatic authority", code)
	}
	for _, bad := range []string{
		strings.Replace(body, `"expected_authorization_id":"0"`, `"expected_authorization_id":null`, 1),
		strings.Replace(body, `"max_item_gross":"1000"`, `"max_item_gross":"01000"`, 1),
		strings.Replace(body, `"merchant_id":"1"`, `"merchant_id":"01"`, 1),
		strings.Replace(body, `"merchant_id":"1"`, `"MERCHANT_ID":"1"`, 1),
		strings.Replace(body, `"cost_policy":"provider_managed_excluded"`, `"cost_policy":"free"`, 1), strings.Replace(body, `"channel_id":"7"`, `"channel_id":"7","payer":"forged"`, 1), strings.Replace(body, `"channel_id":"7"`, `"channel_id":"7","channel_id":"8"`, 1), strings.Replace(body, `"max_day_items":"1"`, `"max_day_items":1`, 1), body + ` {}`} {
		if code, _ := call("POST", target, bad, ""); code != 400 && code != 422 {
			t.Fatal("ambiguous automatic authority", code)
		}
	}
	if e = identity.NewFundsAuthorization(p, nil).SetPassword(ctx, identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.73"}, "contract-password-123", "", "auto73-funds-password-456", ""); e != nil {
		t.Fatal(e)
	}
	challengeBody, _ := json.Marshal(map[string]any{"target": "/console/v1/admin" + target, "body": json.RawMessage(body)})
	code, challenge := call("POST", "/security/funds-authorizations/challenges", string(challengeBody), "")
	if code != 201 {
		t.Fatal("automatic concrete scope challenge", code, challenge)
	}
	data := challenge["data"].(map[string]any)
	summary := data["summary"].(map[string]any)
	if !strings.Contains(fmt.Sprint(summary["cost_policy"]), "不保证") || !strings.Contains(fmt.Sprint(summary["channels"]), "toaccount.transfer") || strings.Contains(fmt.Sprint(summary), "auto73-private-payee@example.invalid") || strings.Contains(fmt.Sprint(summary), "Auto73 Private Name") {
		t.Fatal("unsafe/incomplete automatic preview", summary)
	}
	confirm, _ := json.Marshal(map[string]any{"challenge_id": data["challenge_id"], "payment_password": "auto73-funds-password-456"})
	code, grant := call("POST", "/security/funds-authorizations/confirm", string(confirm), "")
	if code != 200 {
		t.Fatal("automatic confirmation", code, grant)
	}
	authorization := grant["data"].(map[string]any)["authorization"].(string)
	input["max_item_gross"] = "999"
	changed, _ := json.Marshal(input)
	if code, _ := call("POST", target, string(changed), authorization); code != 428 {
		t.Fatal("changed automatic budget inherited grant", code)
	}
	provider.account = "seller:2088000000000074"
	if code, _ := call("POST", target, body, authorization); code != 428 {
		t.Fatal("changed physical payer inherited grant", code)
	}
	provider.account = "seller:2088000000000073"
	code, created := call("POST", target, body, authorization)
	if code != 201 {
		t.Fatal("independent automatic authority", code, created)
	}
	aid := created["data"].(map[string]any)["id"].(string)
	if code, _ := call("POST", target, body, authorization); code != 409 && code != 428 {
		t.Fatal("automatic grant replay", code)
	}
	if code, _ := call("POST", fmt.Sprintf("/settlement-plans/%d/auto-authorizations/%s/revoke", plan.ID+1, aid), `{}`, ""); code != 404 {
		t.Fatal("foreign plan revoked original authority", code)
	}
	if code, _ := call("POST", target+"/"+aid+"/revoke", `{}`, ""); code != 200 {
		t.Fatal("reduce-only revoke required new funds grant", code)
	}
	var authorities, consumed, journals, claims int
	if e = p.QueryRow(ctx, `SELECT (SELECT count(*) FROM settlement_auto_authorizations),(SELECT count(*) FROM admin_funds_grants WHERE consumed_at IS NOT NULL),(SELECT count(*) FROM journals),(SELECT count(*) FROM settlement_auto_claims)`).Scan(&authorities, &consumed, &journals, &claims); e != nil || authorities != 1 || consumed != 1 || journals != 0 || claims != 0 || provider.writes.Load() != 0 || provider.reads.Load() != 0 {
		t.Fatal("policy operation moved funds/called provider or lost grant CAS", authorities, consumed, journals, claims, e)
	}
}
