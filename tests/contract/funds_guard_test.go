package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestAdminFundsGuardProtectsLocalIntentAndImmutableReplay(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(p, false)
	s.FundsActionResolver = s.ResolveFundsAction
	user := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	f := identity.NewFundsAuthorization(p, nil)
	actor := identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.1"}
	if e = f.SetPassword(ctx, actor, "contract-password-123", "", "local-funds-password-456", ""); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"admin_funds_authorization_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	paid, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "funds-source", Name: "test", Type: "sandbox", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, paid.TradeNo, "funds-source"); e != nil {
		t.Fatal(e)
	}
	req := func(path, body, grant string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if grant != "" {
			r.Header.Set("X-Payment-Authorization", grant)
		}
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	target := "/console/v1/admin/workflows/internal-transfers"
	body := `{"merchant_id":"1","recipient_id":"2","amount":"1.25","external_no":"stable-funds-intent"}`
	if w := req(target, body, ""); w.Code != 428 {
		t.Fatal("missing grant not blocked", w.Code, w.Body.String())
	}
	var count int
	if e = p.QueryRow(ctx, "SELECT count(*) FROM ledger_requests").Scan(&count); e != nil || count != 0 {
		t.Fatal("unauthorized reserved funds", count, e)
	}
	authorize := func(body string) string {
		t.Helper()
		preview, _ := json.Marshal(map[string]any{"target": target, "body": json.RawMessage(body)})
		w := req("/console/v1/admin/security/funds-authorizations/challenges", string(preview), "")
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
		var c struct{ Data identity.FundsChallenge }
		json.Unmarshal(w.Body.Bytes(), &c)
		confirm := fmt.Sprintf(`{"challenge_id":%q,"payment_password":"local-funds-password-456"}`, c.Data.ChallengeID)
		w = req("/console/v1/admin/security/funds-authorizations/confirm", confirm, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var g struct{ Data identity.FundsGrant }
		json.Unmarshal(w.Body.Bytes(), &g)
		return g.Data.Authorization
	}
	grant := authorize(body)
	changed := strings.Replace(body, "1.25", "2.00", 1)
	if w := req(target, changed, grant); w.Code != 428 {
		t.Fatal("amount switched", w.Code, w.Body.String())
	}
	w := req(target, body, grant)
	if w.Code != 201 {
		t.Fatal("authorized intent failed", w.Code, w.Body.String())
	}
	// Original same business intent returns before the guard; a lost HTTP result
	// never forces a second authorization or a different external number.
	if w = req(target, body, ""); w.Code != 201 {
		t.Fatal("immutable replay requires new grant", w.Code, w.Body.String())
	}
	if w = req("/console/v1/admin/workflows/internal-transfers/01/approve", `{}`, ""); w.Code != 428 {
		t.Fatal("numeric path alias bypassed authorization", w.Code, w.Body.String())
	}
	b, e := s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 875 || b.Reserved != 125 {
		t.Fatal(b, e)
	}
	body2 := strings.Replace(body, "stable-funds-intent", "policy-funds-intent", 1)
	grant = authorize(body2)
	if _, e = p.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{settle_type}','"1"') WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	if w = req(target, body2, grant); w.Code != 428 {
		t.Fatal("changed merchant policy accepted", w.Code, w.Body.String())
	}
	if e = p.QueryRow(ctx, "SELECT count(*) FROM ledger_requests").Scan(&count); e != nil || count != 1 {
		t.Fatal("policy mismatch reserved", count, e)
	}
	var consumed int
	p.QueryRow(ctx, "SELECT count(*) FROM admin_funds_grants WHERE consumed_at IS NOT NULL").Scan(&consumed)
	if consumed != 1 {
		t.Fatal("mismatch consumed grant", consumed)
	}
}
