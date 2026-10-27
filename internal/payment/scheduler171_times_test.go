package payment

import (
	"context"
	"testing"
)

func TestScheduler171PendingManualUsesRealDueAndKeepsPreviousActualStart(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171Configure(t, s, a, "referral_daily")
	scheduler171Pending(t, s, a, "referral_daily")
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	before, e := s.ScheduledTasks(ctx, a, "referral_daily")
	if e != nil || before[0].LastRunTime == nil {
		t.Fatal("actual previous start absent", e)
	}
	started := *before[0].LastRunTime
	pending := scheduler171Pending(t, s, a, "referral_daily")
	after, e := s.ScheduledTasks(ctx, a, "referral_daily")
	if e != nil {
		t.Fatal(e)
	}
	if after[0].LastRunTime == nil || !after[0].LastRunTime.Equal(started) {
		t.Error("pending intent erased previous actual execution time")
	}
	if after[0].NextRunTime == nil || !after[0].NextRunTime.Equal(pending.ScheduledFor) {
		t.Error("manual pending due was replaced with future automatic schedule")
	}
	if after[0].LastStatus != "pending" || after[0].CanRun {
		t.Fatal("pending intent reported an executed or repeatable state")
	}
}
