package payment

import (
	"context"
	"testing"
)

func dailyLedgerClean70(t *testing.T, s *Store) {
	t.Helper()
	r, e := s.CheckLedger(context.Background())
	if e != nil || r.InvalidReferralTerms != 0 || r.InvalidReferralRewards != 0 || r.InvalidReferralClawbacks != 0 || r.InvalidAccounts != 0 || r.InvalidJournals != 0 {
		t.Fatal("daily funded state failed ledger proof", r, e)
	}
}
func TestReferralDaily70LedgerAcceptsOnlyProvenLifecycle(t *testing.T) {
	s, o, _ := dailyFixture70(t)
	ctx := context.Background()
	dailyLedgerClean70(t, s)
	referralPayTestOrder(t, s, o)
	dailyLedgerClean70(t, s)
	r, e := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "daily70-ledger-first", 98)
	if e != nil {
		t.Fatal(e)
	}
	dailyLedgerClean70(t, s)
	if e = s.ResolveRefund(ctx, r.ID, "unknown"); e != nil {
		t.Fatal(e)
	}
	dailyLedgerClean70(t, s)
	if e = s.ResolveRefund(ctx, r.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	dailyLedgerClean70(t, s)
	day, clock := dailyFutureClock70()
	if tick, e := s.tickReferralDailyAt(ctx, day, 10, clock); e != nil || tick.Awarded != 1 {
		t.Fatal(tick, e)
	}
	dailyLedgerClean70(t, s)
	r, e = s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "daily70-ledger-awarded", 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	dailyLedgerClean70(t, s)
}

func TestReferralDaily70LedgerRejectsOrphanEvents(t *testing.T) {
	s, o, _ := dailyFixture70(t)
	ctx := context.Background()
	// Even an authentic event cannot substitute for its missing business fact.
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.insertDailyEvent(ctx, tx, o.ID, "funded", 0, "payment-engine", make([]byte, 32)); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	r, e := s.CheckLedger(ctx)
	if e != nil || r.InvalidReferralTerms+r.InvalidReferralRewards+r.InvalidReferralClawbacks == 0 {
		t.Fatal("orphan authenticated event escaped source checker", r, e)
	}
}

func TestReferralDaily70LedgerRejectsOfflineSourceTampering(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"source_hash", `ALTER TABLE referral_daily_liabilities DISABLE TRIGGER referral_daily_liability_immutable;UPDATE referral_daily_liabilities SET source_hash=decode(repeat('00',32),'hex')`},
		{"missing_audit", `ALTER TABLE referral_daily_events DISABLE TRIGGER referral_daily_event_immutable;DELETE FROM referral_daily_events WHERE operation='funded'`},
		{"orphan_account", `INSERT INTO accounts(owner_id,kind,scope_id)VALUES(0,'referral_daily_pending',9223372036854775000)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, o, _ := dailyFixture70(t)
			ctx := context.Background()
			referralPayTestOrder(t, s, o)
			if _, e := s.pool.Exec(ctx, tc.sql); e != nil {
				t.Fatal(e)
			}
			r, e := s.CheckLedger(ctx)
			if e != nil || r.InvalidReferralTerms+r.InvalidReferralRewards+r.InvalidReferralClawbacks == 0 {
				t.Fatal("offline source corruption accepted", r, e)
			}
		})
	}
}
