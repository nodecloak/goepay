package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/enrollment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestMembershipReferral72HTTPRejectsDelegatedPurchaseAndPolicy(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(p, false)
	adminUser := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	admin, csrf, e := s.Sessions.Create(adminUser)
	if e != nil {
		t.Fatal(e)
	}
	merchant, mcsrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	tx, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	delegated, dcsrf, _, e := s.Sessions.CreateDelegated(ctx, tx, identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1}, adminUser, admin)
	if e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	var gid int64
	if e = p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"local72-free","enabled":true,"price_cents":"0","duration_days":"30"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	body := fmt.Sprintf(`{"group_id":"%d","external_no":"member72-delegated"}`, gid)
	w := lookupHTTP(t, s.Handler(), "POST", "/console/v1/merchant/workflows/memberships", body, delegated, "merchant", dcsrf)
	if w.Code != 403 {
		t.Errorf("delegated membership purchase bypassed original administrator funds: %d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct{ role, token, csrf string }{{"merchant", merchant, mcsrf}, {"merchant", delegated, dcsrf}} {
		if w = lookupHTTP(t, s.Handler(), "GET", "/console/v1/"+tc.role+"/membership-referral-policies/global/0", "", tc.token, tc.role, tc.csrf); w.Code != 403 {
			t.Fatal("non-super policy", w.Code, w.Body.String())
		}
	}
	if w = lookupHTTP(t, s.Handler(), "POST", "/console/v1/admin/membership-referral-policies/global/0", `{"expected":"0","enabled":false,"rate_bps":0}`, admin, "admin", ""); w.Code != 403 {
		t.Fatal("policy csrf", w.Code)
	}
	if w = lookupHTTP(t, s.Handler(), "GET", "/console/v1/admin/membership-referral-policies/global/0", "", admin, "admin", csrf); w.Code != 200 {
		t.Fatal("own super read", w.Code, w.Body.String())
	}
	if data := lookupData(t, w); data["available"] != true || data["revision"] != "0" || data["enabled"] != nil || data["rate_bps"] != nil {
		t.Fatal("default policy shape", data)
	}
	if w = lookupHTTP(t, s.Handler(), "GET", "/console/v1/admin/referral-policies/global/0", "", admin, "admin", csrf); w.Code != 200 || lookupData(t, w)["membership_referral_policy_available"] != true {
		t.Fatal("independent membership policy not advertised", w.Code, w.Body.String())
	}
	if w = lookupHTTP(t, s.Handler(), "POST", "/console/v1/merchant/workflows/memberships", strings.Replace(body, "delegated", "own", 1), merchant, "merchant", mcsrf); w.Code != 201 {
		t.Fatal("own merchant free purchase", w.Code, w.Body.String())
	}
}

func TestMembershipReferral72HTTPIndependentFundsPolicyAndPurchaseSnapshot(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(p, false)
	s.FundsActionResolver = s.ResolveFundsAction
	user := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	auth := identity.NewFundsAuthorization(p, nil)
	if e = auth.SetPassword(ctx, identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.72"}, "contract-password-123", "", "membership72-funds-password", ""); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"admin_funds_authorization_enabled","value":"true"}'),('settings','{"key":"registration_open","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	request := func(target, body, grant string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("X-Payment-Authorization", grant)
		r.RemoteAddr = "192.0.2.72:1234"
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	authorize := func(target, body string) string {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"target": target, "body": json.RawMessage(body)})
		w := request("/console/v1/admin/security/funds-authorizations/challenges", string(raw), "")
		if w.Code != 201 {
			t.Fatal("preview", w.Code, w.Body.String())
		}
		var c struct{ Data identity.FundsChallenge }
		if json.Unmarshal(w.Body.Bytes(), &c) != nil {
			t.Fatal("challenge shape")
		}
		w = request("/console/v1/admin/security/funds-authorizations/confirm", fmt.Sprintf(`{"challenge_id":%q,"payment_password":"membership72-funds-password"}`, c.Data.ChallengeID), "")
		if w.Code != 200 {
			t.Fatal("confirm", w.Code, w.Body.String())
		}
		var g struct{ Data identity.FundsGrant }
		json.Unmarshal(w.Body.Bytes(), &g)
		return g.Data.Authorization
	}
	target := "/console/v1/admin/membership-referral-policies/global/0"
	body := `{"expected":"0","enabled":true,"rate_bps":2500}`
	if w := request(target, body, ""); w.Code != 428 {
		t.Fatal("unconfirmed policy", w.Code, w.Body.String())
	}
	grant := authorize(target, body)
	if w := request(target, strings.Replace(body, "2500", "5000", 1), grant); w.Code != 428 {
		t.Fatal("changed body grant", w.Code, w.Body.String())
	}
	if w := request("/console/v1/admin/referral-policies/global/0", `{"expected":"0","enabled":true,"rate_bps":2500,"mode":"immediate","basis":"fee","refund_policy":"any_successful_refund_voids_reward"}`, grant); w.Code != 428 {
		t.Fatal("membership grant used for ordinary reward", w.Code, w.Body.String())
	}
	if w := request(target, body, grant); w.Code != 200 {
		t.Fatal("authorized policy", w.Code, w.Body.String())
	}
	if w := request(target, body, grant); w.Code != 409 && w.Code != 428 {
		t.Fatal("policy grant replay", w.Code, w.Body.String())
	}
	issued, e := s.Store.RotateReferralToken(ctx, 1, 0, payment.TransferBatchActor{Role: "merchant", ID: 1, Name: "synthetic-promoter"})
	if e != nil {
		t.Fatal(e)
	}
	var code string
	en, e := enrollment.New(p, s.Store, make([]byte, 32), enrollment.SenderFunc(func(_ context.Context, _, _, v string) error { code = v; return nil }))
	if e != nil {
		t.Fatal(e)
	}
	ch, e := en.Issue(ctx, enrollment.VerificationRequest{Purpose: "register", Email: "membership72@example.test", Source: "192.0.2.72"})
	if e != nil {
		t.Fatal(e)
	}
	buyer, e := en.Register(ctx, enrollment.RegisterInput{ChallengeID: ch.ID, Code: code, Email: "membership72@example.test", Source: "192.0.2.72", Username: "membership72", Name: "local buyer", Password: "membership72-local-password", PromotionToken: issued.Token})
	if e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: buyer.MerchantID, OutTradeNo: "membership72-original-credit", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "membership72-local-credit"); e != nil {
		t.Fatal(e)
	}
	var gid int64
	if e = p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"member72-paid","enabled":true,"price_cents":"300","duration_days":"30"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	buyTarget := "/console/v1/admin/workflows/memberships"
	buyBody := fmt.Sprintf(`{"merchant_id":"%d","group_id":"%d","external_no":"member72-balance-buy"}`, buyer.MerchantID, gid)
	before, summary, e := s.ResolveFundsAction(ctx, p, buyTarget, json.RawMessage(buyBody))
	if e != nil || summary["membership_reward_cents"] != "75" || summary["membership_reward_reason"] != "eligible" {
		t.Fatal("private funds snapshot and safe summary", summary, e)
	}
	raw, _ := json.Marshal(summary)
	if bytes.Contains(raw, []byte("proof")) || bytes.Contains(raw, []byte("fingerprint")) {
		t.Fatal("private proof leaked into summary")
	}
	purchaseGrant := authorize(buyTarget, buyBody)
	changed := `{"expected":"1","enabled":true,"rate_bps":5000}`
	if w := request(target, changed, authorize(target, changed)); w.Code != 200 {
		t.Fatal("change policy", w.Code, w.Body.String())
	}
	after, _, e := s.ResolveFundsAction(ctx, p, buyTarget, json.RawMessage(buyBody))
	if e != nil || bytes.Equal(before.Digest(), after.Digest()) {
		t.Fatal("purchase funds digest ignored reward policy", e)
	}
	if w := request(buyTarget, buyBody, purchaseGrant); w.Code != 428 {
		t.Fatal("stale purchase authorization", w.Code, w.Body.String())
	}
	if w := request(buyTarget, buyBody, authorize(buyTarget, buyBody)); w.Code != 201 {
		t.Fatal("authorized purchase", w.Code, w.Body.String())
	}
	if w := request(buyTarget, buyBody, ""); w.Code != 201 {
		t.Fatal("original purchase replay", w.Code, w.Body.String())
	}
	var price, reward, income, count int64
	e = p.QueryRow(ctx, `SELECT s.price,s.reward_amount,a.balance,(SELECT count(*) FROM membership_referral_rewards) FROM membership_referral_sources s JOIN accounts a ON a.id=s.income_account_id WHERE s.merchant_id=$1`, buyer.MerchantID).Scan(&price, &reward, &income, &count)
	if e != nil || price != 300 || reward != 150 || income != 150 || count != 1 {
		t.Fatal("original budget/once reward", price, reward, income, count, e)
	}
	b, e := s.Store.Balance(ctx, buyer.MerchantID)
	if e != nil || b.Available != 700 {
		t.Fatal("buyer duplicate debit", b, e)
	}
	b, e = s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 150 {
		t.Fatal("promoter once credit", b, e)
	}
	check, e := s.Store.CheckLedger(ctx)
	if e != nil || check.InvalidReferralTerms != 0 || check.InvalidReferralRewards != 0 || check.InvalidJournals != 0 || check.InvalidAccounts != 0 {
		t.Fatal("ledger check", check, e)
	}
}
