package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMembershipSaleUXHTTPFundsGrantBindsQuantityAndConfiguration(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(pool, false)
	s.FundsActionResolver = s.ResolveFundsAction
	user := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	funds := identity.NewFundsAuthorization(pool, nil)
	if e = funds.SetPassword(ctx, identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.1"}, "contract-password-123", "", "sale-funds-password-456", ""); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"admin_funds_authorization_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	var group int64
	if e = pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"sale funds","enabled":true,"price_cents":"100","duration_months":1}') RETURNING id`).Scan(&group); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "sale-funds-test", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "local-sale-funds-test"); e != nil {
		t.Fatal(e)
	}
	request := func(path, body, grant string) *httptest.ResponseRecorder {
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
	target := "/console/v1/admin/workflows/memberships"
	body := fmt.Sprintf(`{"merchant_id":"1","group_id":"%d","external_no":"sale-http-once","quantity":2}`, group)
	grantFor := func(body string) string {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"target": target, "body": json.RawMessage(body)})
		w := request("/console/v1/admin/security/funds-authorizations/challenges", string(raw), "")
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
		var challenge struct{ Data identity.FundsChallenge }
		json.Unmarshal(w.Body.Bytes(), &challenge)
		w = request("/console/v1/admin/security/funds-authorizations/confirm", fmt.Sprintf(`{"challenge_id":%q,"payment_password":"sale-funds-password-456"}`, challenge.Data.ChallengeID), "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var grant struct{ Data identity.FundsGrant }
		json.Unmarshal(w.Body.Bytes(), &grant)
		return grant.Data.Authorization
	}
	if w := request(target, body, ""); w.Code != 428 {
		t.Fatalf("ungranted purchase: %d %s", w.Code, w.Body.String())
	}
	grant := grantFor(body)
	if w := request(target, strings.Replace(body, `"quantity":2`, `"quantity":3`, 1), grant); w.Code != 428 {
		t.Fatalf("grant quantity not bound: %d %s", w.Code, w.Body.String())
	}
	w := request(target, body, grant)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(target, body, ""); w.Code != 201 {
		t.Fatalf("original purchase replay consumed another grant: %d %s", w.Code, w.Body.String())
	}
	if w = request(target, strings.Replace(body, `"quantity":2`, `"quantity":3`, 1), ""); w.Code != 409 {
		t.Fatalf("original request quantity changed: %d %s", w.Code, w.Body.String())
	}
	b, e := s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 800 {
		t.Fatalf("quantity not charged once: %+v %v", b, e)
	}
	changed := strings.Replace(body, "sale-http-once", "sale-http-stale", 1)
	stale := grantFor(changed)
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{price_cents}','"200"') WHERE id=$1`, group); e != nil {
		t.Fatal(e)
	}
	if w = request(target, changed, stale); w.Code != 428 {
		t.Fatalf("grant ignored new price: %d %s", w.Code, w.Body.String())
	}
	var purchases, providerOperations int
	if e = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM membership_purchases),(SELECT count(*) FROM provider_operations)").Scan(&purchases, &providerOperations); e != nil || purchases != 1 || providerOperations != 0 {
		t.Fatalf("failed guard leaked intent/network: %d %d %v", purchases, providerOperations, e)
	}
}
