package payment

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"goepay/internal/platform/jobs"
)

func TestScheduler171SharingAdmitsOnlyOriginalBoundedQueriesNeverFirstSend(t *testing.T) {
	f := newSharingExecutionFixture(t)
	s := f.Store
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	for _, name := range []string{"scheduler171-sharing-a", "scheduler171-sharing-b"} {
		o := f.paid(t, name, false)
		if _, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, name, channelSharingActor); e != nil {
			t.Fatal(e)
		}
	}
	beforeSends, beforeQueries := f.Sends.Load(), f.Queries.Load()
	if _, e := s.pool.Exec(ctx, `UPDATE jobs SET run_at=clock_timestamp()-interval '1 second' WHERE kind='channel-sharing.reconcile'`); e != nil {
		t.Fatal(e)
	}
	scheduler171ConfigureManaged(t, s, a, "profitsharing", false)
	q := jobs.Queue{Pool: s.pool, Policy: s}
	if _, e := q.Claim(ctx, []string{"channel-sharing.reconcile"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("disabled split query was claimed", e)
	}
	current, e := s.ScheduledTasks(ctx, a, "profitsharing")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UpdateScheduledTask(ctx, a, "profitsharing", ScheduledTaskInput{Enabled: true, Schedule: ScheduledTaskSchedule{"once", 1, time.Now().UTC().Add(time.Hour).Truncate(time.Second)}, Params: ScheduledTaskParams{"profitsharing_limit": 1, "settle_query_limit": 1}, ExpectedFingerprint: current[0].Fingerprint}); e != nil {
		t.Fatal(e)
	}
	scheduler171Pending(t, s, a, "profitsharing")
	tick, e := s.RunScheduledTasksOnce(ctx, 20)
	if e != nil || tick.Completed != 1 || tick.Failed != 0 {
		t.Fatal("original query source task did not execute", tick, e)
	}
	if f.Sends.Load() != beforeSends || f.Queries.Load() != beforeQueries {
		t.Fatal("scheduler performed upstream I/O")
	}
	j, e := q.Claim(ctx, []string{"channel-sharing.reconcile"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.VerifyScheduledJobClaim(ctx, j); e != nil {
		t.Fatal(e)
	}
	var payload struct {
		ID int64 `json:"sharing_id"`
	}
	if e = json.Unmarshal(j.Payload, &payload); e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(1)
	reader := f.Flow
	reader.AllowLive = false
	if result, e := reader.ReconcileChannelSharing(ctx, payload.ID); e != nil || !result.Completed {
		t.Fatal("actual original signed query not consumed", result, e)
	}
	if e = q.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
	if _, e = q.Claim(ctx, []string{"channel-sharing.reconcile"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("profitsharing_limit was not actually consumed", e)
	}
	if f.Sends.Load() != beforeSends || f.Queries.Load() != beforeQueries+1 {
		t.Fatal("first send was recreated", f.Sends.Load(), f.Queries.Load())
	}
}

func TestScheduler171DelayedSettlementQueryConsumesSecondOriginalParameter(t *testing.T) {
	f := newSharingFinishFixture(t)
	s := f.Store
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	first, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "scheduler171-pf", channelSharingActor)
	if e != nil || first.ID <= 0 {
		t.Fatal(first, e)
	}
	beforeSends, beforeQueries := f.Sends.Load(), f.Queries.Load()
	if _, e = s.pool.Exec(ctx, `UPDATE jobs SET run_at=clock_timestamp()-interval '1 second' WHERE kind='channel-sharing-finish.reconcile'`); e != nil {
		t.Fatal(e)
	}
	scheduler171ConfigureManaged(t, s, a, "profitsharing", false)
	q := jobs.Queue{Pool: s.pool, Policy: s}
	if _, e = q.Claim(ctx, []string{"channel-sharing-finish.reconcile"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("disabled PF query was claimed", e)
	}
	v, e := s.ScheduledTasks(ctx, a, "profitsharing")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UpdateScheduledTask(ctx, a, "profitsharing", ScheduledTaskInput{Enabled: true, Schedule: *v[0].Schedule, Params: ScheduledTaskParams{"profitsharing_limit": 1, "settle_query_limit": 1}, ExpectedFingerprint: v[0].Fingerprint}); e != nil {
		t.Fatal(e)
	}
	scheduler171Pending(t, s, a, "profitsharing")
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	j, e := q.Claim(ctx, []string{"channel-sharing-finish.reconcile"}, time.Minute)
	if e != nil {
		t.Fatal("second original query phase not admitted", e)
	}
	if e = s.VerifyScheduledJobClaim(ctx, j); e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(1)
	reader := f.Flow
	reader.AllowLive = false
	if final, e := reader.ReconcileChannelSharingFinish(ctx, first.ID); e != nil || !final.FundsReleased || final.ID != first.ID || final.OutNo != first.OutNo {
		t.Fatal(final, e)
	}
	if f.Sends.Load() != beforeSends || f.Queries.Load() != beforeQueries+1 {
		t.Fatal("scheduler resent original unfreeze instead of signed query")
	}
}
