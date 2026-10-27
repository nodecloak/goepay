package payment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goepay/internal/identity"
)

func scheduler171Admin(t *testing.T, s *Store) MerchantCredentialActor {
	t.Helper()
	if _, e := s.pool.Exec(context.Background(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('scheduler171','admin',171,'Synthetic Scheduler','unused-hash','super')`); e != nil {
		t.Fatal(e)
	}
	user := identity.User{Username: "scheduler171", ID: 171, Role: "admin", AdminClass: "super", AuthVersion: 1}
	sessions := identity.NewPersistentSessions(s.pool, false)
	token, _, e := sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	return MerchantCredentialActor{User: user, SessionToken: token}
}
func scheduler171Configure(t *testing.T, s *Store, a MerchantCredentialActor, key string) ScheduledTaskView {
	t.Helper()
	v, e := s.ScheduledTasks(context.Background(), a, key)
	if e != nil {
		t.Fatal(e)
	}
	saved, e := s.UpdateScheduledTask(context.Background(), a, key, ScheduledTaskInput{Enabled: true, Schedule: ScheduledTaskSchedule{"once", 1, time.Now().UTC().Add(time.Hour).Truncate(time.Second)}, Params: ScheduledTaskParams{"limit": 20}, ExpectedFingerprint: v[0].Fingerprint})
	if e != nil {
		t.Fatal(e)
	}
	return saved
}
func scheduler171Pending(t *testing.T, s *Store, a MerchantCredentialActor, key string) ScheduledTaskRunView {
	t.Helper()
	v, e := s.ScheduledTasks(context.Background(), a, key)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.RequestScheduledTaskRun(context.Background(), a, key, v[0].Fingerprint)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestScheduler171PersistentLeaseFencesOldOwnerAndKeepsOriginalIntent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171Configure(t, s, a, "referral_daily")
	pending := scheduler171Pending(t, s, a, "referral_daily")
	conn, e := s.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	lock := "goepay/scheduled-task/referral_daily"
	if _, e = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, lock); e != nil {
		t.Fatal(e)
	}
	first, ok, e := s.claimScheduledRun(ctx, conn, "referral_daily", 150*time.Millisecond)
	if e != nil || !ok || first.id != pending.ID {
		t.Fatal("did not claim original pending intent", e)
	}
	time.Sleep(180 * time.Millisecond)
	if e = s.renewScheduledLease(ctx, first, time.Second); !errors.Is(e, ErrConflict) {
		t.Fatal("expired owner renewed lease", e)
	}
	if tick, e := s.RunScheduledTasksOnce(ctx, 2); e != nil || tick.Claimed != 0 {
		t.Fatal("live session lock allowed overlapping takeover", tick, e)
	}
	if _, e = conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lock); e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, lock); e != nil {
		t.Fatal(e)
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lock)
	second, ok, e := s.claimScheduledRun(ctx, conn, "referral_daily", time.Second)
	if e != nil || !ok || second.id != first.id || second.epoch != first.epoch+1 || bytes.Equal(second.token, first.token) {
		t.Fatal("reclaim created another intent or reused owner fence", e)
	}
	if e = s.completeScheduledLease(ctx, first, ScheduledTaskCounts{}, nil); !errors.Is(e, ErrConflict) {
		t.Fatal("stale owner completed replacement run", e)
	}
	counts, e := s.executeScheduledTask(ctx, second)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.completeScheduledLease(ctx, second, counts, nil); e != nil {
		t.Fatal(e)
	}
	page, e := s.ScheduledTaskRuns(ctx, a, "referral_daily", 0, 20)
	if e != nil || len(page.Items) != 1 || page.Items[0].Status != "success" || page.Items[0].CompletedAt == nil {
		t.Fatal("original intent not completed once", page, e)
	}
}
func TestScheduler171ConcurrentWorkersConsumeOnlyOneManualIntent(t *testing.T) {
	s := testStore(t)
	a := scheduler171Admin(t, s)
	scheduler171Configure(t, s, a, "referral_daily")
	scheduler171Pending(t, s, a, "referral_daily")
	var claimed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tick, e := s.RunScheduledTasksOnce(context.Background(), 2)
			if e != nil {
				t.Error(e)
				return
			}
			claimed.Add(int64(tick.Claimed))
		}()
	}
	wg.Wait()
	if claimed.Load() != 1 {
		t.Fatal("workers executed original intent more than once", claimed.Load())
	}
	var runs, results int
	if e := s.pool.QueryRow(context.Background(), `SELECT(SELECT count(*) FROM scheduled_task_runs),(SELECT count(*) FROM scheduled_task_results)`).Scan(&runs, &results); e != nil || runs != 1 || results != 1 {
		t.Fatal(runs, results, e)
	}
}
func TestScheduler171ActualBusinessFailureNeverReportsSuccessOrRawError(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171Configure(t, s, a, "referral_daily")
	scheduler171Pending(t, s, a, "referral_daily")
	if _, e := s.pool.Exec(ctx, `ALTER TABLE referral_daily_liabilities RENAME TO scheduler171_private_fault`); e != nil {
		t.Fatal(e)
	}
	tick, e := s.RunScheduledTasksOnce(ctx, 2)
	if e != nil || tick.Failed != 1 || tick.Completed != 0 {
		t.Fatal("handler failure fabricated success", tick, e)
	}
	page, e := s.ScheduledTaskRuns(ctx, a, "referral_daily", 0, 20)
	if e != nil || len(page.Items) != 1 || page.Items[0].Status != "failed" || page.Items[0].ErrorCode != "execution_failed" {
		t.Fatal("raw failure/result projection", page, e)
	}
}
func TestScheduler171RealGenerateOnlyCreatesPendingOnceAndCannotSend(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	plan := settlementPlanReady(t, s, 901)
	incomePaidOrder(t, s, 901, "scheduler171-earned-original")
	if _, e := s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), plan.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	scheduler171Configure(t, s, a, "settlement_generate")
	scheduler171Pending(t, s, a, "settlement_generate")
	tick, e := s.RunScheduledTasksOnce(ctx, 2)
	if e != nil || tick.Completed != 1 || tick.Failed != 0 {
		t.Fatal("real local generation did not execute", tick, e)
	}
	var requests, pending, autoSend int
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*) FROM ledger_requests WHERE kind='settlements'),(SELECT count(*) FROM ledger_requests WHERE kind='settlements' AND status='pending'),(SELECT count(*) FROM jobs WHERE kind LIKE 'settlement.auto%')`).Scan(&requests, &pending, &autoSend); e != nil || requests != 1 || pending != 1 || autoSend != 0 {
		t.Fatal("task did not generate pending only", requests, pending, autoSend, e)
	}
	scheduler171Pending(t, s, a, "settlement_generate")
	if tick, e = s.RunScheduledTasksOnce(ctx, 2); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	var after int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM ledger_requests WHERE kind='settlements'`).Scan(&after); e != nil || after != requests {
		t.Fatal("repeat task bypassed original plan slot idempotency", after, e)
	}
	page, e := s.ScheduledTaskRuns(ctx, a, "settlement_generate", 0, 20)
	if e != nil || len(page.Items) != 2 || page.Items[1].Counts == nil || page.Items[1].Counts.Generated != 1 {
		t.Fatal("actual generation receipt missing", page, e)
	}
}
func TestScheduler171OriginalSettlementSlotCannotBecomeNewSlot(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	plan := settlementPlanReady(t, s, 901)
	incomePaidOrder(t, s, 901, "scheduler171-slot-income")
	if _, e := s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), plan.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	guarded := context.WithValue(ctx, scheduledTaskSettlementSlotKey{}, scheduledTaskSettlementExpected{plan.ID, 0})
	if _, e := s.createSettlementPlanRun(guarded, plan.ID, 1, nil); !errors.Is(e, ErrState) {
		t.Fatal("frozen old slot reselected current slot", e)
	}
	var count int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM settlement_plan_runs`).Scan(&count); e != nil || count != 0 {
		t.Fatal("changed slot created an authority/run", count, e)
	}
}
func TestScheduler171ScheduleUnitsOnceAndExactBoundary(t *testing.T) {
	base := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		frequency         string
		interval, seconds int64
	}{{"second", 3, 3}, {"minute", 2, 120}, {"hour", 2, 7200}, {"day", 2, 172800}} {
		t.Run(tc.frequency, func(t *testing.T) {
			v := ScheduledTaskSchedule{tc.frequency, tc.interval, base}
			slot, at, e := v.due(base.Add(time.Duration(2*tc.seconds)*time.Second), -1)
			if e != nil || slot != 2 || !at.Equal(base.Add(time.Duration(2*tc.seconds)*time.Second)) {
				t.Fatal("wrong frequency unit", slot, at, e)
			}
			if _, _, e = v.due(at, slot); !errors.Is(e, ErrState) {
				t.Fatal("same automatic slot repeated", e)
			}
		})
	}
	once := ScheduledTaskSchedule{"once", 1, base}
	if next := once.next(base, 0); next != nil {
		t.Fatal("once task has another automatic run")
	}
	if _, _, e := once.due(base, 0); !errors.Is(e, ErrState) {
		t.Fatal("once duplicated", e)
	}
	for _, bad := range []ScheduledTaskSchedule{{"second", 0, base}, {"day", 367, base}, {"cron", 1, base}, {"once", 2, base}, {"minute", 1, base.Add(time.Nanosecond)}} {
		if bad.validate() == nil {
			t.Fatal(fmt.Sprintf("invalid schedule accepted: %#v", bad))
		}
	}
}
