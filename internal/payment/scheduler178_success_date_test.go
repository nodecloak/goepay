package payment

import (
	"context"
	"testing"
	"time"

	"goepay/internal/platform/jobs"
)

func TestScheduler178OldCreationPaidTodayStillReceivesOriginalNotifyRetry(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	o := scheduler178LegacyNotifyOrder190(t, s, "old-created-new-paid178", 1)
	scheduler171ConfigureManaged(t, s, a, "notify", true)
	scheduler171Pending(t, s, a, "notify")
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	q := jobs.Queue{Pool: s.pool, Policy: s}
	j, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil || j.EventKey != "notify:"+o.TradeNo {
		t.Fatal("genuine success today was excluded by old creation date", j.EventKey, e)
	}
	if e = q.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
	var admissions, claims int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM scheduled_job_admissions),(SELECT count(*) FROM scheduled_job_claims)`).Scan(&admissions, &claims); e != nil || admissions != 1 || claims != 1 {
		t.Fatal("original retry authority changed", admissions, claims, e)
	}
	if _, e = q.Claim(ctx, []string{"payment.notify"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("same original retry was recreated", e)
	}
}

func TestScheduler178NotifyFailureRecentSuccessUsesPaidDayNotCreation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler178LegacyNotifyOrder190(t, s, "old-created-risk178", 0)
	for key, value := range map[string]string{"auto_check_notify": "1", "check_notify_count": "1", "check_notify_notice": "0"} {
		transferSetting(t, s, key, value)
	}
	scheduler171ConfigureManaged(t, s, a, "notify", true)
	q := jobs.Queue{Pool: s.pool, Policy: s}
	j, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = q.RetryNotificationFailure(ctx, j, "ack", 0); e != nil {
		t.Fatal(e)
	}
	var firstPay, firstStops int
	if e = s.pool.QueryRow(ctx, "SELECT pay,(SELECT count(*) FROM scheduled_notification_stops) FROM merchants WHERE id=901").Scan(&firstPay, &firstStops); e != nil || firstPay != 1 || firstStops != 0 {
		t.Fatal("original first delivery exclusion changed", firstPay, firstStops, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE jobs SET run_at=clock_timestamp()-interval '1 second' WHERE id=$1", j.ID); e != nil {
		t.Fatal(e)
	}
	scheduler171Pending(t, s, a, "notify")
	if tick, err := s.RunScheduledTasksOnce(ctx, 20); err != nil || tick.Completed != 1 {
		t.Fatal(tick, err)
	}
	j, e = q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil || j.Attempts != 2 {
		t.Fatal("real original retry was not claimed", j.Attempts, e)
	}
	if e = q.RetryNotificationFailure(ctx, j, "ack", 0); e != nil {
		t.Fatal(e)
	}
	var pay, stops, failures int
	if e = s.pool.QueryRow(ctx, `SELECT pay,(SELECT count(*) FROM scheduled_notification_stops),(SELECT count(*) FROM scheduled_notification_failures) FROM merchants WHERE id=901`).Scan(&pay, &stops, &failures); e != nil || pay != 0 || stops != 1 || failures != 2 {
		t.Fatal("original three-day success window ignored today's genuine paid receipt", pay, stops, failures, e)
	}
	if e = q.RetryNotificationFailure(ctx, j, "ack", 0); e == nil {
		t.Fatal("consumed original delivery lease recorded another failure")
	}
}
