package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	notice "goepay/internal/merchantnotice"
)

func scheduler171NoticeMerchant(t *testing.T, s *Store, id int64) {
	t.Helper()
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: id, Name: "Synthetic notice scope", APIKey: "synthetic171-only"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||jsonb_build_object('email',$2::text)WHERE id=$1`, id, fmt.Sprintf("synthetic171-%d@example.test", id)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.notice.SaveSettings(ctx, id, 0, notice.SettingsWrite{Preferences: notice.Preferences{DailyReport: 2}}, notice.Actor{Role: "merchant", ID: id, Name: "Synthetic Owner"}); e != nil {
		t.Fatal(e)
	}
}
func scheduler171DailyTask(t *testing.T, s *Store, a MerchantCredentialActor) {
	t.Helper()
	v, e := s.ScheduledTasks(context.Background(), a, "daily_report")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UpdateScheduledTask(context.Background(), a, "daily_report", ScheduledTaskInput{Enabled: true, Schedule: ScheduledTaskSchedule{"once", 1, time.Now().UTC().Add(time.Hour).Truncate(time.Second)}, Params: ScheduledTaskParams{"send_hour": 0}, ExpectedFingerprint: v[0].Fingerprint}); e != nil {
		t.Fatal(e)
	}
}
func TestScheduler171NoticeCheckpointResumesFrozenMerchantsWithoutNewScope(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171NoticeMerchant(t, s, 901)
	scheduler171NoticeMerchant(t, s, 902)
	scheduler171DailyTask(t, s, a)
	pending := scheduler171Pending(t, s, a, "daily_report")
	conn, e := s.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	r, ok, e := s.claimScheduledRun(ctx, conn, "daily_report", 300*time.Millisecond)
	conn.Release()
	if e != nil || !ok || len(r.facts.Merchants) != 2 {
		t.Fatal("original notice scope not frozen", e)
	}
	recorded, e := s.notice.GenerateScheduledSceneMerchant(ctx, 901, 0, s, "daily_report", r.facts.BusinessDate)
	if e != nil || !recorded {
		t.Fatal(e)
	}
	if e = s.checkpointScheduledRun(ctx, r, 1, ScheduledTaskCounts{Examined: 1, Enqueued: 1}); e != nil {
		t.Fatal(e)
	}
	scheduler171NoticeMerchant(t, s, 903)
	time.Sleep(330 * time.Millisecond)
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal("original checkpoint not recovered", tick, e)
	}
	page, e := s.ScheduledTaskRuns(ctx, a, "daily_report", 0, 20)
	if e != nil || len(page.Items) != 1 || page.Items[0].ID != pending.ID || page.Items[0].Counts.Examined != 2 || page.Items[0].Counts.Enqueued != 2 {
		t.Fatal("recovery reset progress/intent or widened scope", page, e)
	}
	var sources, newMerchant int
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM merchant_notice_timer_sources),(SELECT count(*)FROM merchant_notice_timer_sources WHERE merchant_id=903)`).Scan(&sources, &newMerchant); e != nil || sources != 2 || newMerchant != 0 {
		t.Fatal("recovery adopted newly-created merchant or duplicated source", sources, newMerchant, e)
	}
}
func TestScheduler171CheckpointWaitExpiryCannotAdvanceAndReplaySourceIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171NoticeMerchant(t, s, 901)
	scheduler171DailyTask(t, s, a)
	scheduler171Pending(t, s, a, "daily_report")
	conn, e := s.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	r, ok, e := s.claimScheduledRun(ctx, conn, "daily_report", 300*time.Millisecond)
	conn.Release()
	if e != nil || !ok {
		t.Fatal(e)
	}
	if _, e = s.notice.GenerateScheduledSceneMerchant(ctx, 901, 0, s, "daily_report", r.facts.BusinessDate); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE SEQUENCE scheduler171_checkpoint_entered;CREATE FUNCTION scheduler171_checkpoint_gate()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('scheduler171_checkpoint_entered');PERFORM pg_sleep(0.5);RETURN NEW;END$$;CREATE TRIGGER scheduler171_checkpoint_gate BEFORE UPDATE OF progress_index ON scheduled_task_runs FOR EACH ROW EXECUTE FUNCTION scheduler171_checkpoint_gate()`); e != nil {
		t.Fatal(e)
	}
	if e = s.checkpointScheduledRun(ctx, r, 1, ScheduledTaskCounts{Examined: 1, Enqueued: 1}); !errors.Is(e, ErrConflict) {
		t.Fatal("expired checkpoint committed", e)
	}
	var progress, sources int
	var entered bool
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT progress_index FROM scheduled_task_runs WHERE id=$1),(SELECT count(*)FROM merchant_notice_timer_sources),(SELECT is_called FROM scheduler171_checkpoint_entered)`, r.id).Scan(&progress, &sources, &entered); e != nil || !entered || progress != 0 || sources != 1 {
		t.Fatal("checkpoint rollback or original source failed", progress, sources, entered, e)
	}
	if _, e = s.pool.Exec(ctx, `DROP TRIGGER scheduler171_checkpoint_gate ON scheduled_task_runs`); e != nil {
		t.Fatal(e)
	}
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	if e = s.pool.QueryRow(ctx, `SELECT count(*)FROM merchant_notice_timer_sources`).Scan(&sources); e != nil || sources != 1 {
		t.Fatal("replay duplicated original committed source", sources, e)
	}
}
func TestScheduler171SceneCannotChangeFrozenBusinessDate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scheduler171NoticeMerchant(t, s, 901)
	var now time.Time
	if e := s.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	yesterday := now.In(settlementScheduleZone).AddDate(0, 0, -1).Format("2006-01-02")
	if _, e := s.notice.GenerateScheduledSceneMerchant(ctx, 901, 0, s, "daily_report", yesterday); !errors.Is(e, notice.ErrState) {
		t.Fatal("old intent switched to today's notification source", e)
	}
	var sources, jobs int
	if e := s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM merchant_notice_timer_sources),(SELECT count(*)FROM jobs WHERE kind='merchant.notice')`).Scan(&sources, &jobs); e != nil || sources != 0 || jobs != 0 {
		t.Fatal("frozen date rejection published a message", sources, jobs, e)
	}
}
