package payment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestReferralDaily70ExplicitPolicyAndUnfundedCreatedOrder(t *testing.T) {
	s, _, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	old := referralCreateTestOrder(t, s, mid, 100, "daily70-old-immediate")
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	before, e := s.readReferralTerms(ctx, tx, old)
	tx.Rollback(ctx)
	if e != nil {
		t.Fatal(e)
	}
	enabled, rate := true, int64(5000)
	input := referralTestPolicy(&enabled, &rate)
	input.Mode = "daily"
	input.Basis = "gross"
	v, e := s.SetReferralPolicy(ctx, "global", 0, 1, input, referralPolicyAdmin)
	if e != nil || v.Mode != "daily" {
		t.Fatal("explicit safe daily policy rejected", v, e)
	}
	tx, e = s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	after, e := s.readReferralTerms(ctx, tx, old)
	tx.Rollback(ctx)
	if e != nil || string(before.ProofHash) != string(after.ProofHash) || after.RewardAmount != 5 {
		t.Fatal("daily policy changed old immediate v1 terms", after, e)
	}
	o := referralCreateTestOrder(t, s, mid, 100, "daily70-created")
	var terms, liabilities, journals, rewards int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM referral_daily_terms WHERE order_id=$1),(SELECT count(*) FROM referral_daily_liabilities WHERE order_id=$1),(SELECT count(*) FROM journals),(SELECT count(*) FROM referral_rewards WHERE order_id=$1)`, o.ID).Scan(&terms, &liabilities, &journals, &rewards); e != nil || terms != 1 || liabilities != 0 || journals != 0 || rewards != 0 {
		t.Fatal("unpaid daily intent invented funding", terms, liabilities, journals, rewards, e)
	}
}

func dailyFixture70(t *testing.T) (*Store, Order, int64) {
	t.Helper()
	s, promoter, mid, _ := referralRewardFixture(t)
	yes, rate := true, int64(5000)
	p := referralTestPolicy(&yes, &rate)
	p.Mode = "daily"
	p.Basis = "gross"
	if _, e := s.SetReferralPolicy(context.Background(), "global", 0, 1, p, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	return s, referralCreateTestOrder(t, s, mid, 100, "daily70-payment"), promoter
}
func TestReferralDaily70VerifiedPaymentFundsOriginalPendingOnce(t *testing.T) {
	s, o, promoter := dailyFixture70(t)
	ctx := context.Background()
	referralPayTestOrder(t, s, o)
	var liabilities, rewards, pending, fees int64
	check := func() {
		t.Helper()
		e := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM referral_daily_liabilities WHERE order_id=$1),(SELECT count(*) FROM referral_rewards WHERE order_id=$1),(SELECT balance FROM accounts WHERE owner_id=0 AND kind='referral_daily_pending' AND scope_id=$1),(SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees' AND scope_id=0)`, o.ID).Scan(&liabilities, &rewards, &pending, &fees)
		if e != nil || liabilities != 1 || rewards != 0 || pending != 10 || fees != 0 {
			t.Fatal("verified daily payment did not fund exact original liability", liabilities, rewards, pending, fees, e)
		}
		b, e := s.Balance(ctx, promoter)
		if e != nil || b.Available != 0 {
			t.Fatal("daily reward credited before eligible day", b, e)
		}
	}
	check()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if e := s.markPaid(ctx, o.TradeNo, fmt.Sprintf("daily70-duplicate:%d", i), true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "LOCAL_" + o.TradeNo}); e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	check()
}
func TestReferralDaily70MissingBirthMarkerCannotFallBackToImmediate(t *testing.T) {
	s, o, _ := dailyFixture70(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `ALTER TABLE referral_daily_events DISABLE TRIGGER referral_daily_event_immutable;ALTER TABLE referral_daily_terms DISABLE TRIGGER referral_daily_terms_immutable;DELETE FROM referral_daily_events;DELETE FROM referral_daily_terms;ALTER TABLE referral_daily_events ENABLE TRIGGER referral_daily_event_immutable;ALTER TABLE referral_daily_terms ENABLE TRIGGER referral_daily_terms_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); e != nil {
		t.Fatal(e)
	}
	if e := s.markPaid(ctx, o.TradeNo, "daily70-missing-marker", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "LOCAL_" + o.TradeNo}); !errors.Is(e, ErrConflict) {
		t.Fatal("missing daily marker downgraded payment to immediate", e)
	}
	var entries int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_entries").Scan(&entries); e != nil || entries != 0 {
		t.Fatal("missing daily source posted money", entries, e)
	}
}
func TestReferralDaily70MetadataVersionCannotBeReinterpreted(t *testing.T) {
	for _, tc := range []struct{ column, value string }{{"source_version", "1"}, {"mode", "'immediate'"}, {"timezone", "'UTC'"}} {
		t.Run(tc.column, func(t *testing.T) {
			s, o, _ := dailyFixture70(t)
			ctx := context.Background()
			sql := "ALTER TABLE referral_daily_terms DISABLE TRIGGER referral_daily_terms_immutable;ALTER TABLE referral_daily_terms DROP CONSTRAINT referral_daily_terms_" + tc.column + "_check;UPDATE referral_daily_terms SET " + tc.column + "=" + tc.value
			if _, e := s.pool.Exec(ctx, sql); e != nil {
				t.Fatal(e)
			}
			tx, e := s.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			if _, e = s.readReferralTerms(ctx, tx, o); !errors.Is(e, ErrConflict) {
				t.Fatal("offline source metadata accepted", tc.column, e)
			}
		})
	}
}
