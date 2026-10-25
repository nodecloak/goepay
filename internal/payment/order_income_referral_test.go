package payment

import (
	"context"
	"errors"
	"testing"
)

func incomeScopeBalance(t *testing.T, s *Store, owner int64, kind string, scope int64) int64 {
	t.Helper()
	var amount int64
	if e := s.pool.QueryRow(context.Background(), "SELECT balance FROM accounts WHERE(owner_id,kind,scope_id)=($1,$2,$3)", owner, kind, scope).Scan(&amount); e != nil {
		t.Fatal(e)
	}
	return amount
}
func TestIncomeFundingHeldRefundPreservesOriginalReferralFeeContribution(t *testing.T) {
	for _, state := range []string{"failed", "succeeded"} {
		t.Run(state, func(t *testing.T) {
			s := incomeFundingFixture(t, false)
			ctx := context.Background()
			referralBoundMerchants(t, s, 6310, 6311)
			referralFeeSetup(t, s, 6311, 1000, "platform")
			enabled, rate := true, int64(5000)
			if _, e := s.SetReferralPolicy(ctx, "global", 0, 0, referralTestPolicy(&enabled, &rate), referralPolicyAdmin); e != nil {
				t.Fatal(e)
			}
			o := referralCreateTestOrder(t, s, 6311, 100, "held-referral")
			referralPayTestOrder(t, s, o)
			h, e := s.FreezeOrderIncome(ctx, 6311, o.TradeNo, "hold", incomeTestActor)
			if e != nil {
				t.Fatal(e)
			}
			r, e := s.ReserveRefund(ctx, 6311, o.OutTradeNo, "refund", 98)
			if e != nil {
				t.Fatal(e)
			}
			if incomeScopeBalance(t, s, 6310, "available", 0) != 0 || incomeScopeBalance(t, s, 0, "referral_clawback_reserve", r.ID) != 2 || incomeScopeBalance(t, s, 0, "fee_reserved", 0) != 8 || incomeScopeBalance(t, s, 6311, "order_income_refund_reserve", r.ID) != 90 {
				t.Fatal("H90/f8/R5/K3 sources were mixed")
			}
			if e = s.ResolveRefund(ctx, r.ID, "unknown"); e != nil {
				t.Fatal(e)
			}
			if _, e = s.UnfreezeOrderIncome(ctx, 6311, o.TradeNo, h.ID, incomeTestActor); !errors.Is(e, ErrState) {
				t.Fatal(e)
			}
			if e = s.ResolveRefund(ctx, r.ID, state); e != nil {
				t.Fatal(e)
			}
			wantReward, wantFee, wantHeld := int64(0), int64(2), int64(0)
			if state == "failed" {
				wantReward, wantFee, wantHeld = 5, 5, 90
			}
			if incomeScopeBalance(t, s, 6310, "available", 0) != wantReward || incomeScopeBalance(t, s, 0, "fees", 0) != wantFee || incomeScopeBalance(t, s, 6311, "order_income_reserve", h.ID) != wantHeld {
				t.Fatal("original fee/reward/principal did not resolve independently")
			}
			report, e := s.CheckLedger(ctx)
			if e != nil || report.InvalidOrderIncomeFundings != 0 || report.InvalidRefundFundings != 0 || report.InvalidReferralClawbacks != 0 || report.InvalidProviderRefundResults != 0 {
				t.Fatal(report, e)
			}
		})
	}
}
