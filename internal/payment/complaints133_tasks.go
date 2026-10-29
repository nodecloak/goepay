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
	Frequency           int        `json:"frequency"` // minutes
	Num                 int        `json:"num"`       // per-run page size
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastRunAt           *time.Time `json:"last_run_at"`
	LastStatus          string     `json:"last_status"` // ok | failed | ""
	LastReason          string     `json:"last_reason"`
	NextRunAt           *time.Time `json:"next_run_at"`
}

// complaintSyncBounds are the bounds the original cron uses (and the
// complaint_tasks133 CHECK constraints): frequency >= 1 minute, num 1..50.
func validComplaintTaskBounds(frequency, num int) bool {
	return frequency >= 1 && frequency <= 1440 && num >= 1 && num <= 50
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
   t.enabled,t.frequency,t.num,t.consecutive_failures,
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
		if e = rows.Scan(&channel, &v.Name, &v.Provider, &v.Enabled, &v.Frequency, &v.Num, &v.ConsecutiveFailures,
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
func (s *Store) UpsertComplaintSyncTask(ctx context.Context, channelID int64, enabled bool, frequency, num int, actorID int64, actorName string) (ComplaintSyncTaskView, error) {
	var out ComplaintSyncTaskView
	if channelID <= 0 || !validComplaintTaskBounds(frequency, num) {
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
	_, e = tx.Exec(ctx, `INSERT INTO complaint_tasks133(channel_id,enabled,frequency,num,next_run_at)
 VALUES($1,$2,$3,$4,CASE WHEN $2 THEN $5::timestamptz ELSE NULL END)
 ON CONFLICT(channel_id) DO UPDATE SET
   enabled=EXCLUDED.enabled,
   frequency=EXCLUDED.frequency,
   num=EXCLUDED.num,
   consecutive_failures=CASE WHEN EXCLUDED.enabled AND NOT complaint_tasks133.enabled THEN 0 ELSE complaint_tasks133.consecutive_failures END,
   last_reason=CASE WHEN EXCLUDED.enabled AND NOT complaint_tasks133.enabled THEN NULL ELSE complaint_tasks133.last_reason END,
   next_run_at=CASE WHEN EXCLUDED.enabled THEN COALESCE(complaint_tasks133.next_run_at,$5::timestamptz) ELSE complaint_tasks133.next_run_at END,
   updated_at=$5::timestamptz`,
		channelID, enabled, frequency, num, now)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)
 VALUES('admin',$1,$2,'complaint-sync-tasks',$3,'configure',jsonb_build_object('enabled',$4::bool,'frequency',$5::int,'num',$6::int,'provider',$7::text))`,
		actorID, actorName, channelID, enabled, frequency, num, provider); e != nil {
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
   t.enabled,t.frequency,t.num,t.consecutive_failures,
   t.last_run_at,COALESCE(t.last_status,''),COALESCE(t.last_reason,''),t.next_run_at
 FROM complaint_tasks133 t
 LEFT JOIN backoffice_records c ON c.resource='channels' AND c.id=t.channel_id
 WHERE t.channel_id=$1`, channelID).Scan(&channel, &v.Name, &v.Provider, &v.Enabled, &v.Frequency, &v.Num,
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
