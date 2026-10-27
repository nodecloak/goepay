package payment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func scheduler171PolicySnapshot(t *testing.T, s *Store, key string) []byte {
	t.Helper()
	var raw []byte
	if e := s.pool.QueryRow(context.Background(), `SELECT to_jsonb(p)FROM scheduled_tasks p WHERE task_key=$1`, key).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return raw
}
func scheduler171RestorePolicy(t *testing.T, s *Store, key string, raw []byte) {
	t.Helper()
	if _, e := s.pool.Exec(context.Background(), `UPDATE scheduled_tasks p SET(enabled,schedule,params,revision,generation,saved_at,last_run_id,last_manual_run_id,auto_slot,proof_hash)=(SELECT r.enabled,r.schedule,r.params,r.revision,r.generation,r.saved_at,r.last_run_id,r.last_manual_run_id,r.auto_slot,r.proof_hash FROM jsonb_populate_record(NULL::scheduled_tasks,$2::jsonb)r)WHERE p.task_key=$1`, key, string(raw)); e != nil {
		t.Fatal(e)
	}
}
func TestScheduler171WholeValidEnabledPolicyRollbackCannotRestartClosedTask(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	v, e := s.ScheduledTasks(ctx, a, "referral_daily")
	if e != nil {
		t.Fatal(e)
	}
	saved, e := s.UpdateScheduledTask(ctx, a, "referral_daily", ScheduledTaskInput{Enabled: true, Schedule: ScheduledTaskSchedule{"once", 1, time.Now().UTC().Add(-time.Second).Truncate(time.Second)}, Params: ScheduledTaskParams{"limit": 20}, ExpectedFingerprint: v[0].Fingerprint})
	if e != nil {
		t.Fatal(e)
	}
	original := scheduler171PolicySnapshot(t, s, "referral_daily")
	if _, e = s.UpdateScheduledTask(ctx, a, "referral_daily", ScheduledTaskInput{Enabled: false, Schedule: *saved.Schedule, Params: ScheduledTaskParams{"limit": 20}, ExpectedFingerprint: saved.Fingerprint}); e != nil {
		t.Fatal(e)
	}
	scheduler171RestorePolicy(t, s, "referral_daily", original)
	if _, e = s.ScheduledTasks(ctx, a, "referral_daily"); !errors.Is(e, ErrConflict) {
		t.Error("whole old legitimate MAC row restored readable enabled policy", e)
	}
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); !errors.Is(e, ErrConflict) || tick.Claimed != 0 {
		t.Error("closed policy rollback executed original local handler", tick, e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, `SELECT count(*)FROM scheduled_task_runs`).Scan(&count); e != nil || count != 0 {
		t.Error("rollback created a new execution intent", count, e)
	}
}
func TestScheduler171AutomaticPrivateHeadRollbackCannotHideOriginalSlot(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	v, e := s.ScheduledTasks(ctx, a, "referral_daily")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UpdateScheduledTask(ctx, a, "referral_daily", ScheduledTaskInput{Enabled: true, Schedule: ScheduledTaskSchedule{"second", 1, time.Now().UTC().Add(-time.Second).Truncate(time.Second)}, Params: ScheduledTaskParams{"limit": 20}, ExpectedFingerprint: v[0].Fingerprint}); e != nil {
		t.Fatal(e)
	}
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	first := scheduler171PolicySnapshot(t, s, "referral_daily")
	time.Sleep(1100 * time.Millisecond)
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	scheduler171RestorePolicy(t, s, "referral_daily", first)
	if _, e = s.ScheduledTasks(ctx, a, "referral_daily"); !errors.Is(e, ErrConflict) {
		t.Fatal("older valid private head/auto slot accepted despite retained later run/result", e)
	}
}
func TestScheduler171CompletedRunCannotBeReclaimedFromItsValidOldPendingRow(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171Configure(t, s, a, "referral_daily")
	pending := scheduler171Pending(t, s, a, "referral_daily")
	var raw []byte
	if e := s.pool.QueryRow(ctx, `SELECT to_jsonb(r)FROM scheduled_task_runs r WHERE id=$1`, pending.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `ALTER TABLE scheduled_task_runs DISABLE TRIGGER scheduled_task_intent_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE scheduled_task_runs r SET(state,lease_token,lease_epoch,lease_until,started_at,progress_index,progress_counts,progress_hash)=(SELECT p.state,p.lease_token,p.lease_epoch,p.lease_until,p.started_at,p.progress_index,p.progress_counts,p.progress_hash FROM jsonb_populate_record(NULL::scheduled_task_runs,$2::jsonb)p)WHERE r.id=$1`, pending.ID, string(raw)); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `ALTER TABLE scheduled_task_runs ENABLE TRIGGER scheduled_task_intent_immutable`); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); !errors.Is(e, ErrConflict) || tick.Claimed != 0 {
		t.Error("completed original intent was reclaimed before discovering retained result", tick, e)
	}
	var epoch int64
	if e = s.pool.QueryRow(ctx, `SELECT lease_epoch FROM scheduled_task_runs WHERE id=$1`, pending.ID).Scan(&epoch); e != nil || epoch != 0 {
		t.Error("new lease committed over completed original result", epoch, e)
	}
}
func TestScheduler171SameValueUnrelatedAuditAndOriginalPendingRemainValid(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	first := scheduler171Configure(t, s, a, "referral_daily")
	second, e := s.UpdateScheduledTask(ctx, a, "referral_daily", ScheduledTaskInput{Enabled: true, Schedule: *first.Schedule, Params: ScheduledTaskParams{"limit": 20}, ExpectedFingerprint: first.Fingerprint})
	if e != nil || second.Revision != first.Revision+1 {
		t.Fatal("same-value actual commit lost", e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',171,'Synthetic','scheduled-tasks',0,'inspect','{"task":"referral_daily","revision":"999"}'),('admin',171,'Synthetic','other-resource',0,'configure','{"task":"referral_daily","revision":"999"}')`); e != nil {
		t.Fatal(e)
	}
	scheduler171Pending(t, s, a, "referral_daily")
	if _, e = s.ScheduledTasks(ctx, a, "referral_daily"); e != nil {
		t.Fatal("unrelated audit invalidated real state", e)
	}
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal("original pending intent could not recover", tick, e)
	}
}

func TestScheduler171RetainedLeaseClaimRejectsWholeEarlierPendingRun(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171Configure(t, s, a, "referral_daily")
	pending := scheduler171Pending(t, s, a, "referral_daily")
	var raw []byte
	if e := s.pool.QueryRow(ctx, `SELECT to_jsonb(r) FROM scheduled_task_runs r WHERE id=$1`, pending.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	conn, e := s.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	if _, ok, e := s.claimScheduledRun(ctx, conn, "referral_daily", time.Minute); e != nil || !ok {
		t.Fatal(ok, e)
	}
	scheduler171RestoreRun(t, s, pending.ID, raw)
	if _, e = s.readScheduledRun(ctx, s.pool, pending.ID); !errors.Is(e, ErrConflict) {
		t.Fatal("old pending row hid an actual retained lease claim", e)
	}
}

func scheduler171RestoreRun(t *testing.T, s *Store, id int64, raw []byte) {
	t.Helper()
	ctx := context.Background()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `ALTER TABLE scheduled_task_runs DISABLE TRIGGER scheduled_task_intent_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE scheduled_task_runs r SET(state,lease_token,lease_epoch,lease_until,started_at,progress_index,progress_counts,progress_hash)=(SELECT p.state,p.lease_token,p.lease_epoch,p.lease_until,p.started_at,p.progress_index,p.progress_counts,p.progress_hash FROM jsonb_populate_record(NULL::scheduled_task_runs,$2::jsonb)p)WHERE r.id=$1`, id, string(raw)); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `ALTER TABLE scheduled_task_runs ENABLE TRIGGER scheduled_task_intent_immutable`); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
}

func TestScheduler171RetainedCheckpointRejectsWholeEarlierProgress(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	if e := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "Synthetic progress", APIKey: "synthetic171-only"}); e != nil {
		t.Fatal(e)
	}
	scheduler171ConfigureManaged(t, s, a, "balance_notice", true)
	pending := scheduler171Pending(t, s, a, "balance_notice")
	conn, e := s.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	r, ok, e := s.claimScheduledRun(ctx, conn, "balance_notice", time.Minute)
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
	var raw []byte
	if e = s.pool.QueryRow(ctx, `SELECT to_jsonb(r)FROM scheduled_task_runs r WHERE id=$1`, pending.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	if e = s.checkpointScheduledRun(ctx, r, 1, ScheduledTaskCounts{Examined: 1}); e != nil {
		t.Fatal(e)
	}
	scheduler171RestoreRun(t, s, pending.ID, raw)
	if _, e = s.readScheduledRun(ctx, s.pool, pending.ID); !errors.Is(e, ErrConflict) {
		t.Fatal("old valid progress MAC hid retained completed checkpoint", e)
	}
}
