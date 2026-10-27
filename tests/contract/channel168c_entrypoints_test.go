package contract_test

import (
	"context"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/server"
)

type channel168FixturePolicy func(*testing.T, *server.Server)

func applyChannel168Fixture(t *testing.T, s *server.Server, policy channel168FixturePolicy) {
	t.Helper()
	policy(t, s)
}

// Keep the original fee/product assertions and additionally require real 99
// admission facts at every route/birth. No external provider is contacted.
func installChannel168CLimits(t *testing.T, s *server.Server) {
	t.Helper()
	installChannel168Fees(t, s)
	ctx := context.Background()
	sessions := identity.NewPersistentSessions(s.Pool, false)
	user := identity.User{ID: 168, Username: "channel168", Role: "admin", AdminClass: "super", AuthVersion: 1}
	token, _, e := sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	actor := payment.MerchantCredentialActor{User: user, SessionToken: token}
	for id := range s.ProviderFlow.Providers {
		b, e := s.ProviderFlow.RuntimeChannel(id)
		if e != nil {
			t.Fatal(e)
		}
		before, e := s.Store.RuntimeChannelLimits(ctx, b, actor)
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.Store.UpdateRuntimeChannelLimits(ctx, b, actor, payment.RuntimeChannelLimitsInput{PayMin: "0.01"}, before.Fingerprint)
		if e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() {
		var routes, facts, bad int
		e := s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments r JOIN runtime_channel_limits l ON l.channel_id=r.channel_id),(SELECT count(*) FROM provider_route_fee_quotes WHERE facts->'GlobalLimits' IS NOT NULL AND facts->'GlobalLimits'<>'null'::jsonb),(SELECT count(*) FROM provider_route_fee_quotes q JOIN runtime_channel_limits l ON l.channel_id=(q.facts->>'ChannelID')::bigint WHERE q.facts->'GlobalLimits' IS NULL OR (q.facts->'GlobalLimits'->>'Channel')::bigint<>(q.facts->>'ChannelID')::bigint OR (q.facts->'GlobalLimits'->>'Gross')::bigint<>(q.facts->>'Amount')::bigint)`).Scan(&routes, &facts, &bad)
		if e != nil || routes == 0 || facts != routes || bad != 0 {
			t.Error("product bypassed authenticated global parent admission", routes, facts, bad, e)
		}
		var missingSuccess int
		e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM orders o JOIN provider_route_fee_quotes q ON q.merchant_id=o.merchant_id AND q.out_trade_no=o.out_trade_no WHERE o.status='paid' AND q.facts->'GlobalLimits' IS NOT NULL AND q.facts->'GlobalLimits'<>'null'::jsonb AND NOT EXISTS(SELECT 1 FROM runtime_channel_success_events e WHERE e.order_id=o.id AND e.channel_id=(q.facts->>'ChannelID')::bigint AND e.realmoney=o.amount)`).Scan(&missingSuccess)
		if e != nil || missingSuccess != 0 {
			t.Error("authenticated product success omitted actual parent gross", missingSuccess, e)
		}
	})
}

func TestChannel168CMiniLaunchHTTPMaterialsHaveOneUseBoundStateButNoPaymentAttempt(t *testing.T) {
	runMiniLaunchHTTPMaterialsHaveOneUseBoundStateButNoPaymentAttemptWithOriginalGroupFixture(t, true, installChannel168CLimits)
}
func TestChannel168CMiniOAuthHTTPConsumesBeforeTLSExchangeAndBindsTrustedPayer(t *testing.T) {
	runMiniOAuthHTTPConsumesBeforeTLSExchangeAndBindsTrustedPayerWithOriginalGroupFixture(t, true, installChannel168CLimits)
}
func TestChannel168CMiniOAuthWechatHTTPLaunchToTrustedJSAPIPayer(t *testing.T) {
	runMiniOAuthWechatHTTPLaunchToTrustedJSAPIPayerWithOriginalGroupFixture(t, true, installChannel168CLimits)
}
func TestChannel168CWebOAuthHTTPStateBrowserAndTrustedPayerSubmission(t *testing.T) {
	runWebOAuthHTTPStateBrowserAndTrustedPayerSubmissionWithOriginalGroupFixture(t, true, installChannel168CLimits)
}
func TestChannel168CWebOAuthWechatHTTPUsesAuthenticatedOpenIDWithoutCreditingPayment(t *testing.T) {
	runWebOAuthWechatHTTPUsesAuthenticatedOpenIDWithoutCreditingPaymentWithOriginalGroupFixture(t, true, installChannel168CLimits)
}
func TestChannel168CCollectionKeepsOriginalRouteAfterGroupChange(t *testing.T) {
	runChannel168CollectionKeepsOriginalRouteAfterGroupChange(t, installChannel168CLimits)
}
func TestChannel168CRechargeRetainsOriginalMaterialAndRejectsExplicitBypass(t *testing.T) {
	runChannel168RechargeRetainsOriginalMaterialAndRejectsExplicitBypass(t, installChannel168CLimits)
}
func TestChannel168CPreauthOnlyChannelUsesDedicatedCreationAndHonestSDKMaterial(t *testing.T) {
	runChannel168PreauthOnlyChannelUsesDedicatedCreationAndHonestSDKMaterial(t, installChannel168CLimits)
}
func TestChannel168CScanHTTPOriginalContractsUnknownRecoveryAndLateNotify(t *testing.T) {
	runChannel168ScanHTTPOriginalContractsUnknownRecoveryAndLateNotify(t, installChannel168CLimits)
}
