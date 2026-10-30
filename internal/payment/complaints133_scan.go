package payment

import (
	"context"
	"strconv"
	"time"
)

// scanComplaintSyncTasks is the generic-tick executor for the "complain"
// scheduled task. The generic scheduler holds NO provider client (see
// RunScheduledTasksOnce), so this step performs NO provider wire I/O itself.
// Instead it ENQUEUES a channel-scoped `complaint.sync` job for each enabled,
// due channel and advances that channel's schedule forward — exactly how the
// other provider-backed work (reconcile, bank recheck) is handed to the job
// worker, which runs it with provider access (ProviderFlow.SyncComplaints).
//
// The enqueue + advance happen in one transaction per tick so a channel is
// neither double-queued within its frequency window (event_key is
// complaint-sync:<channel>:<slot>, ON CONFLICT DO NOTHING) nor advanced without
// a job. A channel at the MAX_FAILURES threshold is already disabled on
// complaint_tasks133 and so is not selected.
//
// complaint_tasks133 may not exist on an install that has not applied
// migration 000133; a missing relation is treated as "no due channels" rather
// than a task failure so the scheduler stays green until the store lands.
func (s *Store) scanComplaintSyncTasks(ctx context.Context, r scheduledTaskRun) (ScheduledTaskCounts, error) {
	var counts ScheduledTaskCounts
	var present bool
	if e := s.pool.QueryRow(ctx, `SELECT to_regclass('complaint_tasks133') IS NOT NULL`).Scan(&present); e != nil {
		return counts, e
	}
	if !present {
		return counts, nil
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return counts, e
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	// Claim every due channel, advance it forward and return its new slot so the
	// enqueued job's event_key is stable per window (idempotent re-ticks). Each
	// row carries its own num (per-run page size) and days (获取条件 window) so the
	// scheduler honours the values the admin set on the 同步任务 page.
	rows, e := tx.Query(ctx, `UPDATE complaint_tasks133
 SET last_run_at=$1,
     next_run_at=$1+make_interval(mins => frequency),
     updated_at=$1
 WHERE enabled AND (next_run_at IS NULL OR next_run_at<=$1)
 RETURNING channel_id,num,days,extract(epoch FROM date_trunc('minute',$1))::bigint`, now)
	if e != nil {
		return counts, e
	}
	type due struct {
		channel, slot int64
		num, days     int
	}
	var dues []due
	for rows.Next() {
		var d due
		if e = rows.Scan(&d.channel, &d.num, &d.days, &d.slot); e != nil {
			rows.Close()
			return counts, e
		}
		if d.num < 1 || d.num > 200 {
			d.num = 20
		}
		dues = append(dues, d)
	}
	if e = rows.Err(); e != nil {
		return counts, e
	}
	rows.Close()
	for _, d := range dues {
		if _, e = tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload,run_at)
 VALUES ($1,'complaint.sync',jsonb_build_object('channel_id',$2::bigint,'num',$3::int,'days',$4::int),clock_timestamp())
 ON CONFLICT DO NOTHING`,
			complaintSyncEventKey(d.channel, d.slot), d.channel, d.num, d.days); e != nil {
			return counts, e
		}
		counts.Examined++
		counts.Enqueued++
	}
	if e = tx.Commit(ctx); e != nil {
		return ScheduledTaskCounts{}, e
	}
	return counts, nil
}

func complaintSyncEventKey(channel, slot int64) string {
	return "complaint-sync:" + strconv.FormatInt(channel, 10) + ":" + strconv.FormatInt(slot, 10)
}
