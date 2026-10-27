package payment

import (
	"context"
	"testing"
	"time"
)

func TestScheduler171SettleActuallyGeneratesPendingWithoutGrantingAutomaticPayment(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	p := settlementPlanReady(t, s, 17410)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 17410, OutTradeNo: "scheduler171-settle-funds", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "synthetic171-settle-paid"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, p.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	scheduler171ConfigureManaged(t, s, a, "settle", true)
	if e = s.ScheduleSettlementPlans(ctx); e != nil {
		t.Fatal(e)
	}
	var oldRuns int
	if e = s.pool.QueryRow(ctx, `SELECT count(*)FROM settlement_plan_runs`).Scan(&oldRuns); e != nil || oldRuns != 0 {
		t.Fatal("legacy scan ignored takeover", oldRuns, e)
	}
	scheduler171Pending(t, s, a, "settle")
	tick, e := s.RunScheduledTasksOnce(ctx, 20)
	if e != nil || tick.Completed != 1 || tick.Failed != 0 {
		t.Fatal("original local generate stage not actually executed", tick, e)
	}
	var requests, transfers int
	var status string
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM ledger_requests),(SELECT count(*)FROM transfers),(SELECT status FROM ledger_requests LIMIT 1)`).Scan(&requests, &transfers, &status); e != nil || requests != 1 || transfers != 0 || status != "pending" {
		t.Fatal("scheduler granted financial permission", requests, transfers, status, e)
	}
	v, e := s.ScheduledTasks(ctx, a, "settle")
	if e != nil {
		t.Fatal(e)
	}
	history, e := s.ScheduledTaskRuns(ctx, a, "settle", 0, 20)
	if e != nil || len(history.Items) != 1 || history.Items[0].Counts == nil || history.Items[0].Counts.Generated != 1 || history.Items[0].Counts.AwaitingAuthorization != 1 {
		t.Fatal("pending automatic authority not reported", history, e)
	}
	if v[0].LastRunTime == nil || v[0].LastRunTime.After(time.Now().Add(time.Second)) {
		t.Fatal("no actual start time")
	}
}
