package contract_test

import (
	"context"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/server"
)

// Run the original local HTTP/TLS product assertions with an actual registered
// parent policy. The original variants remain separate and unconfigured.
func installChannel168Fees(t *testing.T, s *server.Server) {
	t.Helper()
	ctx := context.Background()
	if _, e := s.Pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('channel168','admin',168,'Synthetic fee operator','unused-hash','super');INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	sessions := identity.NewPersistentSessions(s.Pool, false)
	user := identity.User{ID: 168, Username: "channel168", Role: "admin", AdminClass: "super", AuthVersion: 1}
	token, _, e := sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	actor := payment.MerchantCredentialActor{User: user, SessionToken: token}
	for id := range s.ProviderFlow.Providers {
		binding, e := s.ProviderFlow.RuntimeChannel(id)
		if e != nil {
			t.Fatal(e)
		}
		state, e := s.Store.RuntimeChannelPolicies(ctx, []payment.RuntimeChannel{binding}, actor)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Store.UpdateRuntimeChannelPolicy(ctx, binding, actor, "99.50", 0, true, state[0].Fingerprint); e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() {
		var routes, orders, terms, bad int
		e := s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM orders),(SELECT count(*) FROM order_route_fee_terms),(SELECT count(*) FROM orders o LEFT JOIN order_route_fee_terms t ON t.order_id=o.id LEFT JOIN provider_route_fee_quotes q ON q.merchant_id=o.merchant_id AND q.out_trade_no=o.out_trade_no WHERE t.order_id IS NULL OR (q.facts->>'ParentConfigured'='true' AND (o.fee_rate_bps<>50 OR o.fee_policy<>'merchant_deduct')))`).Scan(&routes, &orders, &terms, &bad)
		if e != nil || routes == 0 || orders == 0 || terms != orders || bad != 0 {
			t.Error("configured product did not consume its sealed parent fee", routes, orders, terms, bad, e)
		}
	})
}
