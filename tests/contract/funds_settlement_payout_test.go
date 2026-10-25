package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestFundsAuthorizationBindsWalletSettlementExecuteAndCancel(t *testing.T) {
	s, p, key := fixture(t)
	ctx := context.Background()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "settlement-http-app", PrivateKey: key, PublicKey: &key.PublicKey})
	if e != nil {
		t.Fatal(e)
	}
	provider := &settlementHTTPProvider{Provider: ali}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: provider}, AllowLive: true}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	s.FundsActionResolver = s.ResolveFundsAction
	s.Sessions = identity.NewPersistentSessions(p, false)
	if _, e = p.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"3"}' WHERE id=1; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}'),('settings','{"key":"settlement_fee_rate_bps","value":"250"}'),('settings','{"key":"admin_funds_authorization_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "alipay", Account: "wallet-settlement-12345678", Name: "Temporary Name"}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant"}); e != nil {
		t.Fatal(e)
	}
	user := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	if e = identity.NewFundsAuthorization(p, nil).SetPassword(ctx, identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.1"}, "contract-password-123", "", "wallet-funds-password-456", ""); e != nil {
		t.Fatal(e)
	}
	req := func(target, body, grant string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", target, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Payment-Authorization", grant)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	create := func(no string) int64 {
		t.Helper()
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: no + "-source", Amount: 1000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, no+"-source"); e != nil {
			t.Fatal(e)
		}
		r, e := s.Store.RequestFunds(ctx, "settlements", 1, 0, no, 1000, "merchant:1")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Store.ReviewFundsRequest(ctx, r.ID, true, "admin:1"); e != nil {
			t.Fatal(e)
		}
		w := req(fmt.Sprintf("/console/v1/admin/settlement-payouts/%d/prepare", r.ID), `{"channel_id":"7"}`, "")
		if w.Code != 200 {
			t.Fatal("prepare should not consume funds grant", w.Code, w.Body.String())
		}
		return r.ID
	}
	authorize := func(target string) (string, map[string]string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"target": target, "body": map[string]any{}})
		w := req("/console/v1/admin/security/funds-authorizations/challenges", string(raw), "")
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
		var c struct {
			Data struct {
				identity.FundsChallenge
				Summary map[string]string
			}
		}
		json.Unmarshal(w.Body.Bytes(), &c)
		w = req("/console/v1/admin/security/funds-authorizations/confirm", fmt.Sprintf(`{"challenge_id":%q,"payment_password":"wallet-funds-password-456"}`, c.Data.ChallengeID), "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var g struct{ Data identity.FundsGrant }
		json.Unmarshal(w.Body.Bytes(), &g)
		return g.Data.Authorization, c.Data.Summary
	}
	id := create("wallet-execute")
	target := fmt.Sprintf("/console/v1/admin/settlement-payouts/%d/execute", id)
	if w := req(target, `{}`, ""); w.Code != 428 {
		t.Fatal("wallet paid without grant", w.Code, w.Body.String())
	}
	if provider.writes.Load() != 0 {
		t.Fatal("unauthorized external payout")
	}
	grant, summary := authorize(target)
	if summary["funding_source"] != "settlement" || summary["gross_amount"] != "10.00" || summary["fee_amount"] != "0.25" || summary["net_amount"] != "9.75" || summary["recipient"] != "***5678" {
		t.Fatal("settlement confirmation missing original facts", summary)
	}
	if w := req(fmt.Sprintf("/console/v1/admin/settlement-payouts/%d/cancel-unsent", id), `{}`, grant); w.Code != 428 {
		t.Fatal("execute grant used for cancel", w.Code, w.Body.String())
	}
	w := req(target, `{}`, grant)
	if w.Code != 202 || provider.writes.Load() != 1 {
		t.Fatal(w.Code, w.Body.String(), provider.writes.Load())
	}
	if w = req(target, `{}`, ""); w.Code != 200 || provider.writes.Load() != 1 {
		t.Fatal("claimed payout reauthorization resent", w.Code, w.Body.String())
	}
	w = req(fmt.Sprintf("/console/v1/admin/settlement-payouts/%d/query", id), `{}`, "")
	if w.Code != 200 || provider.reads.Load() != 1 {
		t.Fatal("query blocked by funds password", w.Code, w.Body.String())
	}
	canceled := create("wallet-cancel")
	cancelTarget := fmt.Sprintf("/console/v1/admin/settlement-payouts/%d/cancel-unsent", canceled)
	if w = req(cancelTarget, `{}`, ""); w.Code != 428 {
		t.Fatal("unsent principal released without grant", w.Code, w.Body.String())
	}
	grant, _ = authorize(cancelTarget)
	if w = req(cancelTarget, `{}`, grant); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = req(cancelTarget, `{}`, ""); w.Code != 200 {
		t.Fatal("cancel replay requires new grant", w.Code, w.Body.String())
	}
	b, e := s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 1000 || b.Reserved != 0 {
		t.Fatal("wallet source double debited/released", b, e)
	}
	var consumed int
	p.QueryRow(ctx, "SELECT count(*) FROM admin_funds_grants WHERE consumed_at IS NOT NULL").Scan(&consumed)
	if consumed != 2 {
		t.Fatal("wallet grants", consumed)
	}
}
