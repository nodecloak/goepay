package payment

import (
	"context"
	"errors"
	"testing"
)

func TestReferralClawbackReviewUnsignedProviderFailureCannotReleaseKeyedUnknown(t *testing.T) {
	for _, projection := range []string{"failed", "succeeded"} {
		t.Run(projection, func(t *testing.T) {
			f := newReferralClawbackTLS(t)
			ctx := context.Background()
			r, err := f.Flow.Refund(ctx, 902, f.Order.TradeNo, "review-unsigned-operation", 98)
			if err == nil {
				t.Fatal("fixture expected a lost upstream response")
			}
			referralClawbackBalances(t, f.Store, 0, 8, 2, 0)
			if _, err = f.Store.pool.Exec(ctx, "UPDATE provider_operations SET state=$2 WHERE kind='refund' AND refund_id=$1", r.ID, projection); err != nil {
				t.Fatal(err)
			}
			if _, err = f.Flow.Refund(ctx, 902, f.Order.TradeNo, r.RefundNo, 98); err == nil {
				t.Error("unsigned provider terminal was adopted and sealed as a trusted clawback result")
			}
			referralClawbackBalances(t, f.Store, 0, 8, 2, 0)
			current, err := f.Store.Refund(ctx, 902, r.RefundNo)
			if err != nil || current.Status != "unknown" {
				t.Fatal("cached state changed keyed original refund", current, err)
			}
			f.Phase.Store(4)
			current, err = f.Flow.QueryRefund(ctx, 902, r.RefundNo)
			if err != nil || current.Status != "succeeded" {
				t.Fatal("trusted original query could not repair unsigned operation cache", current, err)
			}
			referralClawbackBalances(t, f.Store, 2, 0, 0, 0)
			if _, err = f.Flow.Refund(ctx, 902, f.Order.TradeNo, r.RefundNo, 98); err != nil {
				t.Fatal(err)
			}
			if f.Sends.Load() != 1 || f.Queries.Load() != 1 {
				t.Fatal("reconciliation resent refund or repeated trusted query", f.Sends.Load(), f.Queries.Load())
			}
			report, err := f.Store.CheckLedger(ctx)
			if err != nil || report.InvalidReferralClawbacks != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
				t.Fatal(report, err)
			}
		})
	}
}

func TestReferralClawbackReviewMerchantFundingShortfallRollsBackRecoveredReward(t *testing.T) {
	s, flow, p, o := referralClawbackFixture(t, "merchant")
	ctx := context.Background()
	// Gross 100, original merchant net 90 and fee/reward 10/5. Merchant-funded
	// full refund needs the merchant's own extra 10; the recovered reward is
	// never a substitute for that principal contribution.
	if _, err := flow.Refund(ctx, 902, o.TradeNo, "review-merchant-shortfall", 100); !errors.Is(err, ErrInsufficient) {
		t.Fatal("reward was used to subsidize merchant-funded principal", err)
	}
	referralClawbackBalances(t, s, 5, 0, 0, 5)
	var refunds, escrows, claws int
	if err := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM refunds),(SELECT count(*) FROM accounts WHERE kind='referral_clawback_reserve'),(SELECT count(*) FROM referral_refund_clawbacks)").Scan(&refunds, &escrows, &claws); err != nil || refunds+escrows+claws != 0 {
		t.Fatal("principal failure retained a partial reward recovery", refunds, escrows, claws, err)
	}
	b, err := s.Balance(ctx, 902)
	if err != nil || b.Available != 90 || b.Reserved != 0 || p.refunds.Load() != 0 {
		t.Fatal("unfunded refund changed original wallet or reached upstream", b, p.refunds.Load(), err)
	}
}

func TestReferralClawbackReviewTrustedFailureReplayRemainsReadOnly(t *testing.T) {
	f := newReferralClawbackTLS(t)
	ctx := context.Background()
	r, err := f.Flow.Refund(ctx, 902, f.Order.TradeNo, "review-trusted-closed", 98)
	if err == nil {
		t.Fatal("fixture expected lost response")
	}
	f.Phase.Store(5)
	r, err = f.Flow.QueryRefund(ctx, 902, r.RefundNo)
	if err != nil || r.Status != "failed" {
		t.Fatal(r, err)
	}
	referralClawbackBalances(t, f.Store, 5, 0, 0, 5)
	for i := 0; i < 3; i++ {
		r, err = f.Flow.Refund(ctx, 902, f.Order.TradeNo, r.RefundNo, 98)
		if err != nil || r.Status != "failed" {
			t.Fatal(r, err)
		}
	}
	if f.Sends.Load() != 1 || f.Queries.Load() != 1 {
		t.Fatal("definitive failed replay performed financial I/O", f.Sends.Load(), f.Queries.Load())
	}
	referralClawbackBalances(t, f.Store, 5, 0, 0, 5)
}

func TestReferralClawbackReviewUnrelatedFeesCannotReduceOriginalRecovery(t *testing.T) {
	s, _, _, original := referralClawbackFixture(t, "platform")
	ctx := context.Background()
	other := referralCreateTestOrder(t, s, 902, 1000, "review-unrelated-fee-income")
	referralPayTestOrder(t, s, other)
	referralClawbackBalances(t, s, 55, 0, 0, 55)
	first, err := s.ReserveRefund(ctx, 902, original.OutTradeNo, "review-pooled-first", 98)
	if err != nil {
		t.Fatal(err)
	}
	// The unrelated order supplied another 50 cents of fee and reward. This
	// refund must still recover only its own R=5 and contribute K=3 to G=8.
	referralClawbackBalances(t, s, 50, 8, 2, 50)
	var reward, contribution, originalFee, priorFee int64
	if err = s.pool.QueryRow(ctx, "SELECT amount,fee_contribution,original_fee,prior_fee_used FROM referral_refund_clawbacks WHERE refund_id=$1", first.ID).Scan(&reward, &contribution, &originalFee, &priorFee); err != nil || reward != 5 || contribution != 3 || originalFee != 10 || priorFee != 0 {
		t.Fatal("pooled fees changed original-order recovery", reward, contribution, originalFee, priorFee, err)
	}
	if err = s.ResolveRefund(ctx, first.ID, "unknown"); err != nil {
		t.Fatal(err)
	}
	referralClawbackBalances(t, s, 50, 8, 2, 50)
	if err = s.ResolveRefund(ctx, first.ID, "failed"); err != nil {
		t.Fatal(err)
	}
	referralClawbackBalances(t, s, 55, 0, 0, 55)
	last, err := s.ReserveRefund(ctx, 902, original.OutTradeNo, "review-pooled-retry", 100)
	if err != nil {
		t.Fatal(err)
	}
	referralClawbackBalances(t, s, 50, 10, 0, 50)
	if err = s.ResolveRefund(ctx, last.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	referralClawbackBalances(t, s, 50, 0, 0, 50)
	var amount int64
	if err = s.pool.QueryRow(ctx, "SELECT COALESCE(sum(amount),0) FROM referral_refund_clawbacks WHERE state='succeeded'").Scan(&amount); err != nil || amount != 5 {
		t.Fatal("another order's reward was revoked", amount, err)
	}
	b, err := s.Balance(ctx, 902)
	if err != nil || b.Available != 900 || b.Reserved != 0 {
		t.Fatal("another order's net income was spent", b, err)
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidReferralClawbacks != 0 || report.InvalidReferralRewards != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatal(report, err)
	}
}
