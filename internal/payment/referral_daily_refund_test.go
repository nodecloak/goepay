package payment

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestReferralDaily70RefundAndTickSerializeOriginalLiability(t *testing.T) {
	s, o, promoter := dailyFixture70(t)
	ctx := context.Background()
	referralPayTestOrder(t, s, o)
	day, clock := dailyFutureClock70()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var refund Refund
	var refundErr, tickErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		refund, refundErr = s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "daily70-race-refund", 98)
	}()
	go func() { defer wg.Done(); <-start; _, tickErr = s.tickReferralDailyAt(ctx, day, 10, clock) }()
	close(start)
	wg.Wait()
	if refundErr != nil || tickErr != nil {
		t.Fatal(refundErr, tickErr)
	}
	if e := s.ResolveRefund(ctx, refund.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	b, e := s.Balance(ctx, promoter)
	if e != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatal("refund/tick double-spent original fee budget", b, e)
	}
	if tick, e := s.tickReferralDailyAt(ctx, day, 10, clock); e != nil || tick.Awarded != 0 {
		t.Fatal(tick, e)
	}
	dailyLedgerClean70(t, s)
}

func TestReferralDaily70RefundTerminalAuditFaultRollsBackWholeFunding(t *testing.T) {
	s, o, _ := dailyFixture70(t)
	ctx := context.Background()
	referralPayTestOrder(t, s, o)
	r, e := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "daily70-audit-refund", 98)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION daily70_fail_refund_audit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='refund-failed' THEN RAISE EXCEPTION 'synthetic audit fault'; END IF;RETURN NEW;END$$;CREATE TRIGGER daily70_refund_audit_fault BEFORE INSERT ON referral_daily_events FOR EACH ROW EXECUTE FUNCTION daily70_fail_refund_audit()`); e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "failed"); e == nil {
		t.Fatal("terminal refund committed without audit")
	}
	var status, state string
	var pending, escrow, feeHeld int64
	e = s.pool.QueryRow(ctx, `SELECT r.status,c.state,p.balance,es.balance,fr.balance FROM refunds r JOIN referral_daily_refund_reservations c ON c.refund_id=r.id JOIN accounts p ON p.id=c.pending_account_id JOIN accounts es ON es.id=c.escrow_account_id JOIN accounts fr ON fr.id=c.fee_reserved_account_id WHERE r.id=$1`, r.ID).Scan(&status, &state, &pending, &escrow, &feeHeld)
	if e != nil || status != "pending" || state != "pending" || pending != 0 || escrow != 2 || feeHeld != 8 {
		t.Fatal("audit fault partially released pending liability", status, state, pending, escrow, feeHeld, e)
	}
	if _, e = s.pool.Exec(ctx, `DROP TRIGGER daily70_refund_audit_fault ON referral_daily_events`); e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	dailyLedgerClean70(t, s)
}

func TestReferralDaily70PendingRefundFeeBudgetBoundaries(t *testing.T) {
	for _, tc := range []struct{ refund, fee, k int64 }{{1, 0, 0}, {98, 8, 3}, {100, 10, 5}} {
		t.Run(fmt.Sprint(tc.refund), func(t *testing.T) {
			s, _, mid, _ := referralRewardFixture(t)
			ctx := context.Background()
			yes, rate := true, int64(5000)
			policy := referralTestPolicy(&yes, &rate)
			policy.Mode = "daily"
			if _, e := s.SetReferralPolicy(ctx, "global", 0, 1, policy, referralPolicyAdmin); e != nil {
				t.Fatal(e)
			}
			o := referralCreateTestOrder(t, s, mid, 100, "daily70-boundary")
			referralPayTestOrder(t, s, o)
			q, e := s.ReferralRefundPreview(ctx, s.pool, o.ID, "daily70-boundary-failed", tc.refund)
			if e != nil || q.Amount != 5 || q.RefundFee != tc.fee || q.FeeContribution != tc.k {
				t.Fatal(q, e)
			}
			r, e := s.ReserveRefund(ctx, mid, o.OutTradeNo, "daily70-boundary-failed", tc.refund)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.ResolveRefund(ctx, r.ID, "failed"); e != nil {
				t.Fatal(e)
			}
			r, e = s.ReserveRefund(ctx, mid, o.OutTradeNo, "daily70-boundary-success", tc.refund)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
				t.Fatal(e)
			}
			var pending, escrow, fees int64
			if e = s.pool.QueryRow(ctx, `SELECT (SELECT balance FROM accounts WHERE owner_id=0 AND kind='referral_daily_pending' AND scope_id=$1),(SELECT sum(balance) FROM accounts WHERE kind='referral_daily_refund_reserve'),(SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees' AND scope_id=0)`, o.ID).Scan(&pending, &escrow, &fees); e != nil || pending != 0 || escrow != 0 || fees != 10-tc.fee {
				t.Fatal(pending, escrow, fees, e)
			}
			dailyLedgerClean70(t, s)
		})
	}
}

func TestReferralDaily70FreshQuoteBindsFailedRefundHistory(t *testing.T) {
	s, o, _ := dailyFixture70(t)
	ctx := context.Background()
	referralPayTestOrder(t, s, o)
	before, e := s.ReferralRefundPreview(ctx, s.pool, o.ID, "daily70-future-quote", 98)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "daily70-intervening-refund", 98)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "unknown"); e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	after, e := s.ReferralRefundPreview(ctx, s.pool, o.ID, "daily70-future-quote", 98)
	if e != nil {
		t.Fatal(e)
	}
	if after.ClawbackRefundID != r.ID || len(after.RequestHash) != 32 || len(after.ResultHash) != 32 ||
		(after.ClawbackRefundID == before.ClawbackRefundID && bytes.Equal(after.RequestHash, before.RequestHash) && bytes.Equal(after.ResultHash, before.ResultHash)) {
		t.Fatal("pending quote revived after intervening unknown/failed history", before, after)
	}
}

func TestReferralDaily70PendingRefundUnknownFailedAndSuccessfulVoid(t *testing.T) {
	s, o, promoter := dailyFixture70(t)
	ctx := context.Background()
	referralPayTestOrder(t, s, o)
	day, clock := dailyFutureClock70()
	quote, e := s.ReferralRefundPreview(ctx, s.pool, o.ID, "daily70-r1", 98)
	if e != nil || quote.Amount != 10 || quote.RewardID != 0 || quote.FeeContribution != 8 || quote.ClawbackState != "daily_pending" {
		t.Fatal("daily pending refund quote", quote, e)
	}
	r, e := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "daily70-r1", 98)
	if e != nil || r.FeeAmount != 8 {
		t.Fatal("daily original pending refund", r, e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "unknown"); e != nil {
		t.Fatal(e)
	}
	if tick, e := s.tickReferralDailyAt(ctx, day, 10, clock); e != nil || tick.Awarded != 0 {
		t.Fatal("unknown refund released pending reward", tick, e)
	}
	var pending, escrow, fees, held int64
	check := func(wantPending, wantEscrow, wantFees, wantHeld int64) {
		t.Helper()
		e := s.pool.QueryRow(ctx, `SELECT (SELECT balance FROM accounts WHERE owner_id=0 AND kind='referral_daily_pending' AND scope_id=$1),COALESCE((SELECT sum(balance) FROM accounts WHERE kind='referral_daily_refund_reserve'),0),(SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees' AND scope_id=0),(SELECT balance FROM accounts WHERE owner_id=0 AND kind='fee_reserved' AND scope_id=0)`, o.ID).Scan(&pending, &escrow, &fees, &held)
		if e != nil || pending != wantPending || escrow != wantEscrow || fees != wantFees || held != wantHeld {
			t.Fatal("daily refund funding", pending, escrow, fees, held, e)
		}
	}
	check(0, 2, 0, 8)
	if replay, e := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "daily70-r1", 98); e != nil || replay.ID != r.ID {
		t.Fatal("daily unknown replay", replay, e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	check(10, 0, 0, 0)
	b, e := s.Balance(ctx, promoter)
	if e != nil || b.Available != 0 {
		t.Fatal("unawarded refund touched promoter wallet", b, e)
	}
	partial, e := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "daily70-r2", 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, partial.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	check(0, 0, 10, 0)
	if tick, e := s.tickReferralDailyAt(ctx, day, 10, clock); e != nil || tick.Awarded != 0 {
		t.Fatal("successful pending refund revived reward", tick, e)
	}
	rest, e := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "daily70-r3", 99)
	if e != nil || rest.FeeAmount != 10 {
		t.Fatal(rest, e)
	}
	if e = s.ResolveRefund(ctx, rest.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	check(0, 0, 0, 0)
	var reservations, rewards int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM referral_daily_refund_reservations WHERE order_id=$1),(SELECT count(*) FROM referral_rewards WHERE order_id=$1)`, o.ID).Scan(&reservations, &rewards); e != nil || reservations != 2 || rewards != 0 {
		t.Fatal("daily void recovered twice", reservations, rewards, e)
	}
}
func TestReferralDaily70FailedPendingRefundThenAwardUsesOriginalClawback(t *testing.T) {
	s, o, promoter := dailyFixture70(t)
	ctx := context.Background()
	referralPayTestOrder(t, s, o)
	day, clock := dailyFutureClock70()
	first, e := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "daily70-failed-before-award", 98)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, first.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	if tick, e := s.tickReferralDailyAt(ctx, day, 10, clock); e != nil || tick.Awarded != 1 {
		t.Fatal("failed pending refund prevented valid daily award", tick, e)
	}
	second, e := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "daily70-after-award", 1)
	if e != nil {
		t.Fatal("real awarded refund rejected prior keyed pending failure", e)
	}
	if e = s.ResolveRefund(ctx, second.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM referral_refund_clawbacks WHERE refund_id=$1`, second.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("awarded daily refund bypassed original clawback", count, e)
	}
	b, e := s.Balance(ctx, promoter)
	if e != nil || b.Available != 0 {
		t.Fatal("awarded daily partial refund did not void full original reward", b, e)
	}
}

func TestReferralDaily70LegacyWithoutTermsDoesNotAcquireRewardOrLoseRefunds(t *testing.T) {
	s, _, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	// Explicit pre-57 raw shape: never born through the current source engine.
	o, err := scanOrder(s.pool.QueryRow(ctx, "INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount) VALUES(nextval('trade_no_seq')::text,$1,'daily70-legacy-without-terms','legacy','wxpay',100,100) RETURNING "+orderColumns, mid))
	if err != nil {
		t.Fatal(err)
	}
	referralPayTestOrder(t, s, o)
	if _, e := s.ReferralRefundPreview(ctx, s.pool, o.ID, "daily70-legacy-refund", 1); e != nil {
		t.Fatal("legacy no-terms refund preview regressed", e)
	}
	r, e := s.ReserveRefund(ctx, mid, o.OutTradeNo, "daily70-legacy-refund", 1)
	if e != nil {
		t.Fatal("legacy no-terms refund rejected", e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	var rewards int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM referral_rewards WHERE order_id=$1`, o.ID).Scan(&rewards); e != nil || rewards != 0 {
		t.Fatal("legacy missing terms received retroactive reward", rewards, e)
	}
}
func TestReferralDaily70BoundedTickDoesNotStarveBehindVoidedLiabilities(t *testing.T) {
	s, first, _ := dailyFixture70(t)
	ctx := context.Background()
	referralPayTestOrder(t, s, first)
	r, e := s.ReserveRefund(ctx, first.MerchantID, first.OutTradeNo, "daily70-void-for-scan", 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	next := referralCreateTestOrder(t, s, first.MerchantID, 100, "daily70-later-pending")
	referralPayTestOrder(t, s, next)
	day, clock := dailyFutureClock70()
	if tick, e := s.tickReferralDailyAt(ctx, day, 1, clock); e != nil || tick.Awarded != 1 || len(tick.OrderIDs) != 1 {
		t.Fatal("bounded scan stranded pending behind terminal void", tick, e)
	}
}
