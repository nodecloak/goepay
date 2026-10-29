package payment

import (
	"context"
	"testing"
	"time"
)

// The "complain" scheduled task is now AVAILABLE (registry: no unavailable
// reason). It configures, schedules and runs like the other admit-style tasks,
// and its generic-tick executor advances every enabled, due channel in
// complaint_tasks133 forward by its configured frequency without any provider
// I/O. The live provider pull is the console sync path's job; this verifies the
// scheduler integration and the forward-only schedule advance.

func TestComplainScheduledTaskIsAvailableAndSchedulable(t *testing.T) {
	if _, ok := scheduledDefinition("complain"); !ok {
		t.Fatal("complain task missing from registry")
	}
	d, _ := scheduledDefinition("complain")
	if d.unavailable != "" {
		t.Fatalf("complain task still unavailable: %q", d.unavailable)
	}
	params, ok := ScheduledTaskParameters("complain")
	if !ok || len(params) != 1 || params[0].Name != "sync_num" {
		t.Fatalf("complain params not registered: %+v", params)
	}
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	// Configure + schedule the task (proves d.unavailable == "" is honoured by
	// UpdateScheduledTask / RequestScheduledTaskRun, which both reject
	// unavailable tasks with ErrInvalid).
	v, e := s.ScheduledTasks(ctx, a, "complain")
	if e != nil {
		t.Fatal(e)
	}
	if !v[0].Available {
		t.Fatal("complain task view not available")
	}
	saved, e := s.UpdateScheduledTask(ctx, a, "complain", ScheduledTaskInput{
		Enabled:             true,
		Schedule:            ScheduledTaskSchedule{Frequency: "once", Interval: 1, ExecuteAt: time.Now().UTC().Add(time.Hour).Truncate(time.Second)},
		Params:              ScheduledTaskParams{"sync_num": 20},
		ExpectedFingerprint: v[0].Fingerprint,
	})
	if e != nil {
		t.Fatal("configure complain:", e)
	}
	if !saved.Configured || !saved.Enabled {
		t.Fatal("complain task not configured/enabled")
	}
	if _, e = s.RequestScheduledTaskRun(ctx, a, "complain", saved.Fingerprint); e != nil {
		t.Fatal("request run complain:", e)
	}
}

func TestComplainScanAdvancesDueChannelsForwardOnly(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	// complaint_tasks133 exists on the migrated test DB. Seed one due channel
	// (next_run_at in the past) and one not-yet-due channel.
	if _, e := s.pool.Exec(ctx, `INSERT INTO complaint_tasks133(channel_id,enabled,frequency,num,next_run_at)
 VALUES (90001,true,6,20,now()-interval '1 hour'),(90002,true,6,20,now()+interval '1 hour')`); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM complaint_tasks133 WHERE channel_id IN (90001,90002)`)
	})
	run := scheduledTaskRun{facts: scheduledTaskFacts{Policy: scheduledTaskPolicy{Task: "complain", Params: ScheduledTaskParams{"sync_num": 20}}}}
	counts, e := s.scanComplaintSyncTasks(ctx, run)
	if e != nil {
		t.Fatal("scan:", e)
	}
	if counts.Examined != 1 || counts.Enqueued != 1 {
		t.Fatalf("expected exactly the one due channel advanced, got %+v", counts)
	}
	// The due channel's next_run_at moved forward past now; the other untouched.
	var due, notDue time.Time
	var lastDue *time.Time
	if e = s.pool.QueryRow(ctx, `SELECT next_run_at,last_run_at FROM complaint_tasks133 WHERE channel_id=90001`).Scan(&due, &lastDue); e != nil {
		t.Fatal(e)
	}
	if !due.After(time.Now().Add(-time.Minute)) || lastDue == nil {
		t.Fatalf("due channel not advanced: next=%v last=%v", due, lastDue)
	}
	if e = s.pool.QueryRow(ctx, `SELECT next_run_at FROM complaint_tasks133 WHERE channel_id=90002`).Scan(&notDue); e != nil {
		t.Fatal(e)
	}
	if !notDue.After(time.Now().Add(30 * time.Minute)) {
		t.Fatalf("not-yet-due channel was touched: %v", notDue)
	}
}
