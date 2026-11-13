package payment

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/platform/jobs"
)

// This replay test needs one immutable financial period. A minute-frequency
// fixture can legitimately advance to a new source while the provider flow is
// running, especially with the race detector enabled.
func scheduledTransferSinglePeriodRun(t *testing.T, s *Store, mid int64, prepare func()) SettlementPlanRunView {
	t.Helper()
	ctx := context.Background()
	p := settlementPlanReady(t, s, mid)
	prepare()
	var now time.Time
	if e := s.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		t.Fatal(e)
	}
	in := settlementPlanInputFor(mid)
	in.Mode = settlementThresholdMode
	in.Schedule = SettlementScheduleSpec{Frequency: "once", AnchorAt: now.UTC().Truncate(time.Second)}
	p, e := s.UpdateSettlementPlan(ctx, p.ID, p.Revision, in, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "auto_settle_money", "1.00")
	transferSetting(t, s, "transfer_alipay", "7")
	authority := settlementPlanAuthorizationInput(t, s)
	if _, e = s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, p.Revision, authority, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	run, e := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	return run
}

func TestScheduledTransferReplayPeriodBoundaries(t *testing.T) {
	before := time.Date(2026, 11, 13, 4, 43, 59, 0, time.UTC)
	after := before.Add(2 * time.Second)
	anchor := before.Truncate(time.Minute)
	minute := SettlementScheduleSpec{Frequency: "minute", Interval: 1, AnchorAt: anchor}
	firstSlot, firstAt, e := dueSettlementSlot(minute, before)
	if e != nil || firstSlot != 0 || !firstAt.Equal(anchor) {
		t.Fatal("first minute period", firstSlot, firstAt, e)
	}
	nextSlot, nextAt, e := dueSettlementSlot(minute, after)
	if e != nil || nextSlot != 1 || !nextAt.Equal(anchor.Add(time.Minute)) {
		t.Fatal("crossing a minute must produce a different period", nextSlot, nextAt, e)
	}
	once := SettlementScheduleSpec{Frequency: "once", AnchorAt: anchor}
	for _, at := range []time.Time{before, after, after.Add(time.Hour)} {
		slot, scheduledAt, e := dueSettlementSlot(once, at)
		if e != nil || slot != firstSlot || !scheduledAt.Equal(firstAt) {
			t.Fatal("single-period replay changed its identity", at, slot, scheduledAt, e)
		}
	}
}

func TestScheduledTransferCannotGrantFundsByEnablingCron(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	thresholdFundedSingle74(t, s, 17401)
	scheduler171ConfigureManaged(t, s, a, "transfer", true)
	scheduler171Pending(t, s, a, "transfer")
	tick, e := s.RunScheduledTasksOnce(ctx, 20)
	if e != nil || tick.Failed != 1 || tick.Completed != 0 {
		t.Fatal("missing independent authority/runtime did not fail visibly", tick, e)
	}
	var requests, transfers, claims int
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM ledger_requests),(SELECT count(*)FROM transfers),(SELECT count(*)FROM settlement_auto_claims)`).Scan(&requests, &transfers, &claims); e != nil || requests != 0 || transfers != 0 || claims != 0 {
		t.Fatal("cron enable granted funds", requests, transfers, claims, e)
	}
}

func TestScheduledTransferRunsOriginalThresholdAuthorityAndOriginalJob(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	original := scheduledTransferSinglePeriodRun(t, s, 17402, func() {
		if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{transfer_api_enabled}','true')WHERE id=17402`); e != nil {
			t.Fatal(e)
		}
		transferSetting(t, s, "transfer_enabled", "true")
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 17402, OutTradeNo: "scheduler171-threshold-funds", Amount: 1000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.MarkPaid(ctx, o.TradeNo, "synthetic171-threshold-paid"); e != nil {
			t.Fatal(e)
		}
	})
	provider := &autoScopeProvider73{kind: "alipay", account: "seller:2088000000000073", product: "alipay.fund.trans.toaccount.transfer"}
	provider.unknown = true
	flow := ProviderFlow{Store: s, AllowLive: true, MerchantChannels: map[int64][]int64{17402: {7}}, Providers: map[int64]adapter.Provider{7: provider}}
	generationRecord, e := s.readSettlementPlanAuthorization(ctx, s.pool, original.AuthorizationID)
	if e != nil {
		t.Fatal(e)
	}
	generation := generationRecord.View
	guard := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, _ string) error {
		if kind != "settlement-auto-authorize" {
			return ErrInvalid
		}
		return nil
	})
	authority, e := flow.AuthorizeSettlementAuto(guard, original.PlanID, original.PlanRevision, SettlementAutoInput{GenerationAuthorizationID: generation.ID, ValidFrom: generation.ValidFrom, ExpiresAt: generation.ExpiresAt, MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 1, CostPolicy: "provider_managed_excluded", Channels: []SettlementAutoChannel{{MerchantID: 17402, ChannelID: 7}}}, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	scheduler171ConfigureManaged(t, s, a, "transfer", true)
	scheduler171Pending(t, s, a, "transfer")
	tick, e := s.RunScheduledTasksOnceWithAutoQueue(ctx, 20, &flow)
	if e != nil || tick.Completed != 1 || tick.Failed != 0 {
		t.Fatal("threshold task did not execute actual source chain", tick, e)
	}
	var requestID int64
	if e = s.pool.QueryRow(ctx, `SELECT request_id FROM settlement_plan_items WHERE run_id=$1 AND state='created'`, original.ID).Scan(&requestID); e != nil {
		t.Fatal(e)
	}
	if provider.transfers.Load() != 0 {
		t.Fatal("scheduler performed provider I/O")
	}
	q := jobs.Queue{Pool: s.pool, Policy: s}
	j, e := q.Claim(ctx, []string{SettlementAutoJobKind}, time.Minute)
	if e != nil {
		t.Fatal("original auto job not admitted", e)
	}
	if e = s.VerifyScheduledJobClaim(ctx, j); e != nil {
		t.Fatal(e)
	}
	if _, e = flow.ExecuteSettlementAutoAuthorized(ctx, requestID, authority.ID, nil); e == nil || provider.transfers.Load() != 1 {
		t.Fatal("actual original fake-provider unknown send not exercised", provider.transfers.Load(), e)
	}
	if _, e = flow.ExecuteSettlementAutoAuthorized(ctx, requestID, authority.ID, nil); e != nil || provider.transfers.Load() != 1 {
		t.Fatal("original unknown payout was resent", provider.transfers.Load(), e)
	}
	if e = q.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
	scheduler171Pending(t, s, a, "transfer")
	tick, e = s.RunScheduledTasksOnceWithAutoQueue(ctx, 20, &flow)
	if e != nil || tick.Failed != 0 || provider.transfers.Load() != 1 {
		t.Fatal("same period regenerated an intent", tick, e)
	}
	var requests, transfers, sources, runs int
	var sourceRunID int64
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM ledger_requests),(SELECT count(*)FROM transfers),(SELECT count(*)FROM settlement_threshold_selections),(SELECT count(*)FROM settlement_plan_runs),(SELECT run_id FROM settlement_threshold_selections LIMIT 1)`).Scan(&requests, &transfers, &sources, &runs, &sourceRunID); e != nil || requests != 1 || transfers != 1 || sources != 1 || runs != 1 || sourceRunID != original.ID {
		t.Fatal("same-period amount/source duplicated or changed its original run", requests, transfers, sources, runs, sourceRunID, original.ID, e)
	}
}
