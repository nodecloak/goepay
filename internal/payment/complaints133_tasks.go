package payment

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Per-channel complaint sync-task configuration (complaint_tasks133), joined with
// the channel's name + provider from the backoffice `channels` resource. This is
// the 同步任务 page's data and the 投诉记录 page's 自动获取 toggle. Only a channel
// whose provider supports complaints (wxpay / alipay) may have a task; others are
// refused. All writes are admin-only, gated by complain_open upstream, audited.

// ComplaintSyncTaskView is one row of the 同步任务 list.
type ComplaintSyncTaskView struct {
	ChannelID           string     `json:"channel_id"`
	Name                string     `json:"name"`
	Provider            string     `json:"provider"` // wxpay | alipay
	Enabled             bool       `json:"enabled"`
	Days                int        `json:"days"`      // 获取条件: only channels with traffic in N days (0 = 不限)
	Frequency           int        `json:"frequency"` // minutes
	Num                 int        `json:"num"`       // per-run page size
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastRunAt           *time.Time `json:"last_run_at"`
	LastStatus          string     `json:"last_status"` // ok | failed | ""
	LastReason          string     `json:"last_reason"`
	NextRunAt           *time.Time `json:"next_run_at"`
}

// complaintSyncBounds are the bounds the console editor uses (and the
// complaint_tasks133 CHECK constraints after migration 000175): frequency
// 1..1440 min, num 1..200 (epay_pro's 同步任务 editor offers 20..200). days is
// the 获取条件 window (>= 0; 0 = 不限).
func validComplaintTaskBounds(frequency, num int) bool {
	return frequency >= 1 && frequency <= 1440 && num >= 1 && num <= 200
}

func validComplaintTaskDays(days int) bool {
	return days >= 0 && days <= 3650
}

// channelComplaintProvider returns the channel's provider (wxpay|alipay) if the
// channel exists AND its provider supports complaints, else "" with ErrNotFound
// (unknown channel) or ErrInvalid (provider does not support complaints).
func (s *Store) channelComplaintProvider(ctx context.Context, q pgx.Row) (string, error) {
	var provider string
	if e := q.Scan(&provider); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", e
	}
	if provider != "wxpay" && provider != "alipay" {
		return "", ErrInvalid
	}
	return provider, nil
}

// ComplaintSyncTasks lists every complaint_tasks133 row joined with its channel
// name + provider. Rows whose channel vanished still list (name "-"). Scoped by
// owner is not applicable: tasks are per CHANNEL and administrator-owned.
func (s *Store) ComplaintSyncTasks(ctx context.Context) ([]ComplaintSyncTaskView, error) {
	rows, e := s.pool.Query(ctx, `SELECT t.channel_id,
   COALESCE(c.body->>'name','-'),
   COALESCE(c.body->>'provider',''),
   t.enabled,t.days,t.frequency,t.num,t.consecutive_failures,
   t.last_run_at,COALESCE(t.last_status,''),COALESCE(t.last_reason,''),t.next_run_at
 FROM complaint_tasks133 t
 LEFT JOIN backoffice_records c ON c.resource='channels' AND c.id=t.channel_id
 ORDER BY t.channel_id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ComplaintSyncTaskView{}
	for rows.Next() {
		var v ComplaintSyncTaskView
		var channel int64
		if e = rows.Scan(&channel, &v.Name, &v.Provider, &v.Enabled, &v.Days, &v.Frequency, &v.Num, &v.ConsecutiveFailures,
			&v.LastRunAt, &v.LastStatus, &v.LastReason, &v.NextRunAt); e != nil {
			return nil, e
		}
		v.ChannelID = itoa64(channel)
		out = append(out, v)
	}
	return out, rows.Err()
}

// UpsertComplaintSyncTask creates or updates one channel's sync task. The
// channel's provider MUST support complaints (wxpay|alipay) — others are refused
// with ErrInvalid. Disabling keeps the row and its failure history (only enabled
// flips). Enabling from a failed/auto-stopped state resets the failure counter
// and schedules the next run now. actorID/actorName audit the change.
func (s *Store) UpsertComplaintSyncTask(ctx context.Context, channelID int64, enabled bool, days, frequency, num int, actorID int64, actorName string) (ComplaintSyncTaskView, error) {
	var out ComplaintSyncTaskView
	if channelID <= 0 || !validComplaintTaskBounds(frequency, num) || !validComplaintTaskDays(days) {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	// Verify the channel supports complaints.
	provider, e := s.channelComplaintProvider(ctx, tx.QueryRow(ctx,
		`SELECT body->>'provider' FROM backoffice_records WHERE resource='channels' AND id=$1`, channelID))
	if e != nil {
		return out, e
	}
	now := time.Now().UTC()
	// Upsert. On re-enable, reset the failure counter and clear the stop reason,
	// and schedule the next run now. On disable, keep failures + history.
	_, e = tx.Exec(ctx, `INSERT INTO complaint_tasks133(channel_id,enabled,days,frequency,num,next_run_at)
 VALUES($1,$2,$3,$4,$5,CASE WHEN $2 THEN $6::timestamptz ELSE NULL END)
 ON CONFLICT(channel_id) DO UPDATE SET
   enabled=EXCLUDED.enabled,
   days=EXCLUDED.days,
   frequency=EXCLUDED.frequency,
   num=EXCLUDED.num,
   consecutive_failures=CASE WHEN EXCLUDED.enabled AND NOT complaint_tasks133.enabled THEN 0 ELSE complaint_tasks133.consecutive_failures END,
   last_reason=CASE WHEN EXCLUDED.enabled AND NOT complaint_tasks133.enabled THEN NULL ELSE complaint_tasks133.last_reason END,
   next_run_at=CASE WHEN EXCLUDED.enabled THEN COALESCE(complaint_tasks133.next_run_at,$6::timestamptz) ELSE complaint_tasks133.next_run_at END,
   updated_at=$6::timestamptz`,
		channelID, enabled, days, frequency, num, now)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)
 VALUES('admin',$1,$2,'complaint-sync-tasks',$3,'configure',jsonb_build_object('enabled',$4::bool,'days',$5::int,'frequency',$6::int,'num',$7::int,'provider',$8::text))`,
		actorID, actorName, channelID, enabled, days, frequency, num, provider); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	// Read back the joined view.
	tasks, e := s.complaintSyncTaskByChannel(ctx, channelID)
	if e != nil {
		return out, e
	}
	return tasks, nil
}

func (s *Store) complaintSyncTaskByChannel(ctx context.Context, channelID int64) (ComplaintSyncTaskView, error) {
	var v ComplaintSyncTaskView
	var channel int64
	e := s.pool.QueryRow(ctx, `SELECT t.channel_id,
   COALESCE(c.body->>'name','-'),COALESCE(c.body->>'provider',''),
   t.enabled,t.days,t.frequency,t.num,t.consecutive_failures,
   t.last_run_at,COALESCE(t.last_status,''),COALESCE(t.last_reason,''),t.next_run_at
 FROM complaint_tasks133 t
 LEFT JOIN backoffice_records c ON c.resource='channels' AND c.id=t.channel_id
 WHERE t.channel_id=$1`, channelID).Scan(&channel, &v.Name, &v.Provider, &v.Enabled, &v.Days, &v.Frequency, &v.Num,
		&v.ConsecutiveFailures, &v.LastRunAt, &v.LastStatus, &v.LastReason, &v.NextRunAt)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return v, ErrNotFound
		}
		return v, e
	}
	v.ChannelID = itoa64(channel)
	return v, nil
}

// SetComplaintTaskStatus flips one task's enabled flag (the 同步任务 page's inline
// 状态 switch → setTaskStatus). Enabling a disabled task resets the failure
// counter + stop reason and schedules the next run now, matching the upsert's
// re-enable semantics; disabling keeps the row and its failure history. The
// task must already exist (else ErrNotFound). Admin-only, audited.
func (s *Store) SetComplaintTaskStatus(ctx context.Context, channelID int64, enabled bool, actorID int64, actorName string) (ComplaintSyncTaskView, error) {
	var out ComplaintSyncTaskView
	if channelID <= 0 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	ct, e := tx.Exec(ctx, `UPDATE complaint_tasks133 SET
   enabled=$2,
   consecutive_failures=CASE WHEN $2 AND NOT enabled THEN 0 ELSE consecutive_failures END,
   last_reason=CASE WHEN $2 AND NOT enabled THEN NULL ELSE last_reason END,
   next_run_at=CASE WHEN $2 THEN COALESCE(next_run_at,$3::timestamptz) ELSE next_run_at END,
   updated_at=$3::timestamptz
 WHERE channel_id=$1`, channelID, enabled, now)
	if e != nil {
		return out, e
	}
	if ct.RowsAffected() == 0 {
		return out, ErrNotFound
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)
 VALUES('admin',$1,$2,'complaint-sync-tasks',$3,'set-status',jsonb_build_object('enabled',$4::bool))`,
		actorID, actorName, channelID, enabled); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return s.complaintSyncTaskByChannel(ctx, channelID)
}

// DeleteComplaintSyncTasks removes one or more channels' sync-task configuration
// (the 同步任务 page's per-row 删除 and 批量删除 → delTask/delTasks). Deleting the
// task row stops that channel from being scheduled; the synced complaints
// themselves (complaints133) are untouched, which keeps the append-only
// complaint ledger intact. Returns the number of rows removed. Admin-only,
// audited per channel. Unknown ids are silently skipped (idempotent delete).
func (s *Store) DeleteComplaintSyncTasks(ctx context.Context, channelIDs []int64, actorID int64, actorName string) (int, error) {
	if len(channelIDs) == 0 {
		return 0, ErrInvalid
	}
	for _, id := range channelIDs {
		if id <= 0 {
			return 0, ErrInvalid
		}
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `DELETE FROM complaint_tasks133 WHERE channel_id = ANY($1) RETURNING channel_id`, channelIDs)
	if e != nil {
		return 0, e
	}
	var removed []int64
	for rows.Next() {
		var cid int64
		if e = rows.Scan(&cid); e != nil {
			rows.Close()
			return 0, e
		}
		removed = append(removed, cid)
	}
	if e = rows.Err(); e != nil {
		return 0, e
	}
	rows.Close()
	if len(removed) > 0 {
		if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)
 SELECT 'admin',$1,$2,'complaint-sync-tasks',cid,'delete',jsonb_build_object('channel_id',cid)
 FROM unnest($3::bigint[]) AS cid`, actorID, actorName, removed); e != nil {
			return 0, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, e
	}
	return len(removed), nil
}

// ComplaintSupportedChannels lists channels whose provider supports complaints
// (wxpay|alipay), for the 投诉记录 page's 自动获取 channel picker. Each row carries
// whether a sync task is already enabled for it.
type ComplaintSupportedChannel struct {
	ChannelID string `json:"channel_id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Enabled   bool   `json:"enabled"`
}

func (s *Store) ComplaintSupportedChannels(ctx context.Context) ([]ComplaintSupportedChannel, error) {
	rows, e := s.pool.Query(ctx, `SELECT c.id,COALESCE(c.body->>'name','-'),c.body->>'provider',
   COALESCE(t.enabled,false)
 FROM backoffice_records c
 LEFT JOIN complaint_tasks133 t ON t.channel_id=c.id
 WHERE c.resource='channels' AND c.body->>'provider' IN('wxpay','alipay')
 ORDER BY c.id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ComplaintSupportedChannel{}
	for rows.Next() {
		var ch ComplaintSupportedChannel
		var id int64
		if e = rows.Scan(&id, &ch.Name, &ch.Provider, &ch.Enabled); e != nil {
			return nil, e
		}
		ch.ChannelID = itoa64(id)
		out = append(out, ch)
	}
	return out, rows.Err()
}

// complaintProviderName is a small display helper (wxpay->微信, alipay->支付宝).
func complaintProviderName(p string) string {
	switch strings.ToLower(p) {
	case "wxpay":
		return "微信"
	case "alipay":
		return "支付宝"
	}
	return p
}
