package payment

import (
	"context"
	"testing"
	"time"

	"goepay/internal/platform/jobs"
)

func TestScheduler171NotifyUsesOriginalUTC8YesterdayCalendarBoundary(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	old := scheduler171NotifyOrder(t, s, "before-yesterday", 1)
	edge := scheduler171NotifyOrder(t, s, "at-yesterday", 1)
	if _, e := s.pool.Exec(ctx, `UPDATE orders SET paid_at=(date_trunc('day',clock_timestamp()AT TIME ZONE 'Asia/Shanghai')-interval '1 day')AT TIME ZONE 'Asia/Shanghai'-CASE WHEN id=$1 THEN interval '1 second' ELSE interval '0 seconds' END WHERE id=ANY($2::bigint[])`, old.ID, []int64{old.ID, edge.ID}); e != nil {
		t.Fatal(e)
	}
	scheduler171ConfigureManaged(t, s, a, "notify", true)
	scheduler171Pending(t, s, a, "notify")
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	q := jobs.Queue{Pool: s.pool, Policy: s}
	j, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil || j.EventKey != "notify:"+edge.TradeNo {
		t.Fatal("original calendar cutoff admitted yesterday-minus-one-second", j.EventKey, e)
	}
	if e = q.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
	if _, e = q.Claim(ctx, []string{"payment.notify"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("older than original calendar boundary received retry authority", e)
	}
}
