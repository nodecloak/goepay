package payment

import (
	"context"
	"testing"
	"time"

	"goepay/internal/platform/jobs"
)

func TestScheduler171AllSchedulingTablesLostCannotRestoreLegacyWorker(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171NotifyOrder(t, s, "schema-loss", 1)
	scheduler171ConfigureManaged(t, s, a, "notify", false)
	if _, e := s.pool.Exec(ctx, `DROP TABLE scheduled_job_claims,scheduled_job_admissions,scheduled_task_results,scheduled_task_runs,scheduled_tasks CASCADE`); e != nil {
		t.Fatal(e)
	}
	q := jobs.Queue{Pool: s.pool, Policy: s}
	if _, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute); e == nil || jobs.IsEmpty(e) {
		t.Fatal("current lost schema silently restored legacy notification", e)
	}
	var attempts int
	if e := s.pool.QueryRow(ctx, `SELECT attempts FROM jobs WHERE kind='payment.notify'`).Scan(&attempts); e != nil || attempts != 1 {
		t.Fatal("corrupt schema acquired a new original job lease", attempts, e)
	}
}
