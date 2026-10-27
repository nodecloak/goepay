package payment

import (
	"context"
	"errors"
	"testing"
	"time"

	"goepay/internal/platform/jobs"
)

func TestScheduler171NotificationFiveConfirmedFailuresUseOriginalBackoffAndStop(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171NotifyOrder(t, s, "five-failures", 0)
	scheduler171ConfigureManaged(t, s, a, "notify", true)
	q := jobs.Queue{Pool: s.pool, Policy: s}
	want := []time.Duration{time.Minute, 9 * time.Minute, 40 * time.Minute, time.Hour, 0}
	for i, delay := range want {
		if i > 0 {
			if _, e := s.pool.Exec(ctx, `UPDATE jobs SET run_at=clock_timestamp()-interval '1 second' WHERE kind='payment.notify'`); e != nil {
				t.Fatal(e)
			}
			scheduler171Pending(t, s, a, "notify")
			if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
				t.Fatal(tick, e)
			}
		}
		j, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
		if e != nil {
			t.Fatal(i, e)
		}
		if e = q.RetryNotificationFailure(ctx, j, "ack", 0); e != nil {
			t.Fatal(i, e)
		}
		var state string
		var seconds float64
		var count int
		if e = s.pool.QueryRow(ctx, `SELECT state,EXTRACT(epoch FROM(run_at-clock_timestamp())),(SELECT failure_count FROM scheduled_notification_failures WHERE id=jobs.scheduled_notification_failure_head)FROM jobs WHERE id=$1`, j.ID).Scan(&state, &seconds, &count); e != nil || count != i+1 || seconds > delay.Seconds()+1 || seconds < delay.Seconds()-2 {
			t.Fatal("original confirmed backoff", i, state, seconds, count, e)
		}
		if (i == 4) != (state == "dead") {
			t.Fatal("wrong original terminal budget", i, state)
		}
	}
	if _, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("five-failure job was recreated", e)
	}
}

func TestScheduler171RetainedNotificationFailuresRejectOldPrivateHead(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171NotifyOrder(t, s, "head-rollback", 0)
	scheduler171ConfigureManaged(t, s, a, "notify", true)
	q := jobs.Queue{Pool: s.pool, Policy: s}
	j, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = q.RetryNotificationFailure(ctx, j, "ack", 0); e != nil {
		t.Fatal(e)
	}
	var first int64
	if e = s.pool.QueryRow(ctx, `SELECT scheduled_notification_failure_head FROM jobs WHERE id=$1`, j.ID).Scan(&first); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE jobs SET run_at=clock_timestamp()-interval '1 second' WHERE id=$1`, j.ID); e != nil {
		t.Fatal(e)
	}
	scheduler171Pending(t, s, a, "notify")
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	j, e = q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = q.RetryNotificationFailure(ctx, j, "ack", 0); e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `ALTER TABLE jobs DISABLE TRIGGER scheduled_notification_failure_head_guard`); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE jobs SET scheduled_notification_failure_head=$2 WHERE id=$1`, j.ID, first); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `ALTER TABLE jobs ENABLE TRIGGER scheduled_notification_failure_head_guard`); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.scheduledNotificationHead(ctx, s.pool, j.ID); !errors.Is(e, ErrConflict) {
		t.Fatal("old valid failure head hid retained newer failures", e)
	}
}
