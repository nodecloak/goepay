package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"goepay/internal/identity"
)

type scheduledTaskPolicy struct {
	Task                                 string
	Enabled                              bool
	Schedule                             ScheduledTaskSchedule
	Params                               ScheduledTaskParams
	Revision, Generation                 int64
	SavedAt                              time.Time
	LastRunID, LastManualRunID, AutoSlot int64
	Proof                                []byte
}

func (s *Store) scheduledPolicyProof(p scheduledTaskPolicy) ([]byte, error) {
	return s.referralDigest("scheduler171/config", []any{p.Task, p.Enabled, p.Schedule, p.Params, p.Revision, p.Generation, p.SavedAt.UTC().Format(time.RFC3339Nano), p.LastRunID, p.LastManualRunID, p.AutoSlot})
}
func (s *Store) scheduledPolicy(ctx context.Context, tx pgx.Tx, key string, write bool) (scheduledTaskPolicy, bool, error) {
	p := scheduledTaskPolicy{Task: key, Params: scheduledDefaults(key), AutoSlot: -1}
	suffix := " FOR SHARE"
	if write {
		suffix = " FOR UPDATE"
	}
	var schedule, params []byte
	e := tx.QueryRow(ctx, `SELECT enabled,schedule,params,revision,generation,saved_at,COALESCE(last_run_id,0),COALESCE(last_manual_run_id,0),auto_slot,proof_hash FROM scheduled_tasks WHERE task_key=$1`+suffix, key).Scan(&p.Enabled, &schedule, &params, &p.Revision, &p.Generation, &p.SavedAt, &p.LastRunID, &p.LastManualRunID, &p.AutoSlot, &p.Proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var origin bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE changes->>'task'=$1 AND ((resource='scheduled-tasks' AND operation='configure') OR (resource='scheduled-task-runs' AND operation='intent')))`, key).Scan(&origin); e != nil {
			return p, false, e
		}
		if origin {
			return p, false, ErrConflict
		}
		return p, false, nil
	}
	if e != nil {
		return p, false, e
	}
	p.SavedAt = p.SavedAt.UTC()
	if decodeScheduledJSON(schedule, &p.Schedule) != nil || decodeScheduledJSON(params, &p.Params) != nil || p.Schedule.validate() != nil || !validScheduledParams(key, p.Params) || p.Revision <= 0 || p.Generation <= 0 || p.AutoSlot < -1 {
		return p, false, ErrConflict
	}
	expected, e := s.scheduledPolicyProof(p)
	if e != nil || !hmac.Equal(expected, p.Proof) {
		return p, false, ErrConflict
	}
	if e = s.verifyScheduledPolicyAnchor(ctx, tx, p); e != nil {
		return p, false, e
	}
	return p, true, nil
}
func (s *Store) saveScheduledPolicy(ctx context.Context, tx pgx.Tx, p *scheduledTaskPolicy) error {
	var e error
	p.Proof, e = s.scheduledPolicyProof(*p)
	if e != nil {
		return e
	}
	schedule, e := json.Marshal(p.Schedule)
	if e != nil {
		return e
	}
	params, e := json.Marshal(p.Params)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO scheduled_tasks(task_key,enabled,schedule,params,revision,generation,saved_at,last_run_id,last_manual_run_id,auto_slot,proof_hash)VALUES($1,$2,$3::jsonb,$4::jsonb,$5,$6,$7,NULLIF($8::bigint,0),NULLIF($9::bigint,0),$10,$11)ON CONFLICT(task_key)DO UPDATE SET enabled=EXCLUDED.enabled,schedule=EXCLUDED.schedule,params=EXCLUDED.params,revision=EXCLUDED.revision,generation=EXCLUDED.generation,saved_at=EXCLUDED.saved_at,last_run_id=EXCLUDED.last_run_id,last_manual_run_id=EXCLUDED.last_manual_run_id,auto_slot=EXCLUDED.auto_slot,proof_hash=EXCLUDED.proof_hash`, p.Task, p.Enabled, string(schedule), string(params), p.Revision, p.Generation, p.SavedAt, p.LastRunID, p.LastManualRunID, p.AutoSlot, p.Proof)
	return e
}
func (s *Store) scheduledFingerprint(p scheduledTaskPolicy, configured bool) (string, error) {
	proof, e := s.referralDigest("scheduler171/config-state", []any{p.Task, configured, p.Enabled, p.Schedule, p.Params, p.Revision, p.Generation, p.SavedAt.UTC().Format(time.RFC3339Nano), p.LastManualRunID})
	return hex.EncodeToString(proof), e
}
func scheduledDBNow(ctx context.Context, q referralReader) (time.Time, error) {
	var now time.Time
	e := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now.UTC(), e
}
func scheduledTaskLock(ctx context.Context, tx pgx.Tx, key string) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "goepay/scheduled-task/"+key)
	return e
}
func scheduledActor(ctx context.Context, tx pgx.Tx, a MerchantCredentialActor) error {
	return identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated)
}
func (s *Store) scheduledView(ctx context.Context, tx pgx.Tx, d scheduledTaskDefinition, p scheduledTaskPolicy, configured bool, now time.Time) (ScheduledTaskView, error) {
	v := ScheduledTaskView{Task: d.key, Name: d.name, Description: d.description, Available: d.unavailable == "", UnavailableReason: d.unavailable, ParameterDefinitions: scheduledParameters(d.key), Params: map[string]int{}, Configured: configured, Enabled: p.Enabled, Revision: p.Revision, LastStatus: "never_run"}
	v.ControlMode = "scheduler"
	if !configured && (d.key == "notify" || d.key == "order_query" || d.key == "daily_report" || d.key == "balance_notice" || d.key == "referral_daily" || d.key == "transfer" || d.key == "settle" || d.key == "profitsharing" || d.key == "order") {
		v.ControlMode = "legacy_managed"
	}
	if !v.Available {
		v.ControlMode = "unavailable"
	}
	for _, parameter := range v.ParameterDefinitions {
		v.Params[parameter.Name] = parameter.Default
	}
	var e error
	v.Fingerprint, e = s.scheduledFingerprint(p, configured)
	if e != nil {
		return v, e
	}
	v.CanUpdate = v.Available
	if configured {
		for name, value := range p.Params {
			v.Params[name] = value
		}
		x := p.Schedule
		v.Schedule = &x
		if p.Enabled {
			v.NextRunTime = p.Schedule.next(now, p.AutoSlot)
		}
	}
	if p.LastRunID > 0 {
		r, e := s.readScheduledRun(ctx, tx, p.LastRunID)
		if e != nil {
			return v, e
		}
		if r.facts.Policy.Task != p.Task {
			return v, ErrConflict
		}
		rv, e := s.scheduledRunView(ctx, tx, r, now)
		if e != nil {
			return v, e
		}
		v.LastStatus = rv.Status
		v.LastRunTime = rv.StartedAt
		if rv.StartedAt == nil {
			var previousID int64
			e = tx.QueryRow(ctx, `SELECT id FROM scheduled_task_runs WHERE task_key=$1 AND started_at IS NOT NULL ORDER BY started_at DESC,id DESC LIMIT 1`, p.Task).Scan(&previousID)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return v, e
			}
			if e == nil {
				previous, err := s.readScheduledRun(ctx, tx, previousID)
				if err != nil || previous.facts.Policy.Task != p.Task {
					return v, ErrConflict
				}
				previousView, err := s.scheduledRunView(ctx, tx, previous, now)
				if err != nil {
					return v, err
				}
				v.LastRunTime = previousView.StartedAt
			}
		}
		v.LastError = rv.ErrorCode
		v.Running = rv.Status == "running" || rv.Status == "lease_expired"
		if rv.Status == "pending" {
			v.CanRun = false
			if v.NextRunTime == nil || rv.ScheduledFor.Before(*v.NextRunTime) {
				due := rv.ScheduledFor
				v.NextRunTime = &due
			}
		}
	}
	v.CanUpdate = v.CanUpdate && !v.Running
	v.CanRun = v.Available && configured && p.Enabled && !v.Running && v.LastStatus != "pending"
	return v, nil
}
func (s *Store) ScheduledTasks(ctx context.Context, a MerchantCredentialActor, key string) ([]ScheduledTaskView, error) {
	definitions := scheduledTaskRegistry
	if key != "" {
		d, ok := scheduledDefinition(key)
		if !ok {
			return nil, ErrNotFound
		}
		definitions = []scheduledTaskDefinition{d}
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = scheduledActor(ctx, tx, a); e != nil {
		return nil, e
	}
	now, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return nil, e
	}
	out := make([]ScheduledTaskView, 0, len(definitions))
	for _, d := range definitions {
		p, configured, e := s.scheduledPolicy(ctx, tx, d.key, false)
		if e != nil {
			return nil, e
		}
		v, e := s.scheduledView(ctx, tx, d, p, configured, now)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if e = scheduledActor(ctx, tx, a); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
func scheduledExpected(actual, expected string) bool {
	return len(expected) == 64 && hmac.Equal([]byte(actual), []byte(expected))
}
func (s *Store) UpdateScheduledTask(ctx context.Context, a MerchantCredentialActor, key string, in ScheduledTaskInput) (ScheduledTaskView, error) {
	var out ScheduledTaskView
	d, ok := scheduledDefinition(key)
	if !ok {
		return out, ErrNotFound
	}
	in.Schedule.ExecuteAt = in.Schedule.ExecuteAt.UTC()
	if d.unavailable != "" || in.Schedule.validate() != nil || !validScheduledParams(key, in.Params) {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = scheduledTaskLock(ctx, tx, key); e != nil {
		return out, e
	}
	p, configured, e := s.scheduledPolicy(ctx, tx, key, true)
	if e != nil {
		return out, e
	}
	if e = scheduledActor(ctx, tx, a); e != nil {
		return out, e
	}
	fp, e := s.scheduledFingerprint(p, configured)
	if e != nil {
		return out, e
	}
	if !scheduledExpected(fp, in.ExpectedFingerprint) {
		return out, ErrConflict
	}
	var unfinished bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scheduled_task_runs WHERE task_key=$1 AND state='running')`, key).Scan(&unfinished); e != nil {
		return out, e
	}
	if unfinished {
		return out, ErrState
	}
	if p.Revision == math.MaxInt64 || p.Generation == math.MaxInt64 {
		return out, ErrState
	}
	rows, e := tx.Query(ctx, `SELECT id FROM scheduled_task_runs WHERE task_key=$1 AND state='pending' ORDER BY id`, key)
	if e != nil {
		return out, e
	}
	var cancelIDs []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		cancelIDs = append(cancelIDs, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	now, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return out, e
	}
	for _, id := range cancelIDs {
		r, e := s.readScheduledRun(ctx, tx, id)
		if e != nil {
			return out, e
		}
		if e = s.finishScheduledRunTx(ctx, tx, r, "cancelled", ScheduledTaskCounts{}, "config_changed", now); e != nil {
			return out, e
		}
	}
	p.Enabled, p.Schedule, p.Params = in.Enabled, in.Schedule, in.Params
	p.Revision++
	p.Generation++
	p.SavedAt = now
	p.AutoSlot = -1
	if e = s.saveScheduledPolicy(ctx, tx, &p); e != nil {
		return out, e
	}
	if e = scheduledAudit(ctx, tx, a, key, "configure", p.Revision); e != nil {
		return out, e
	}
	if e = scheduledActor(ctx, tx, a); e != nil {
		return out, e
	}
	out, e = s.scheduledView(ctx, tx, d, p, true, now)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func scheduledAudit(ctx context.Context, tx pgx.Tx, a MerchantCredentialActor, key, operation string, revision int64) error {
	_, e := tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,'scheduled-tasks',0,$3,jsonb_build_object('task',$4::text,'revision',$5::text))`, a.User.ID, a.User.Username, operation, key, fmt.Sprint(revision))
	return e
}
func (s *Store) RequestScheduledTaskRun(ctx context.Context, a MerchantCredentialActor, key, expected string) (ScheduledTaskRunView, error) {
	var out ScheduledTaskRunView
	d, ok := scheduledDefinition(key)
	if !ok {
		return out, ErrNotFound
	}
	if d.unavailable != "" {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = scheduledTaskLock(ctx, tx, key); e != nil {
		return out, e
	}
	p, configured, e := s.scheduledPolicy(ctx, tx, key, true)
	if e != nil {
		return out, e
	}
	if e = scheduledActor(ctx, tx, a); e != nil {
		return out, e
	}
	if !configured || !p.Enabled {
		return out, ErrConflict
	}
	fp, e := s.scheduledFingerprint(p, true)
	if e != nil {
		return out, e
	}
	if !scheduledExpected(fp, expected) {
		return out, ErrConflict
	}
	var active bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scheduled_task_runs WHERE task_key=$1 AND state<>'completed')`, key).Scan(&active); e != nil {
		return out, e
	}
	if active {
		return out, ErrConflict
	}
	if p.Revision == math.MaxInt64 {
		return out, ErrState
	}
	now, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return out, e
	}
	r, e := s.insertScheduledRun(ctx, tx, &p, "manual", -1, now, now)
	if e != nil {
		return out, e
	}
	p.LastManualRunID = r.id
	p.Revision++
	if e = s.saveScheduledPolicy(ctx, tx, &p); e != nil {
		return out, e
	}
	if e = scheduledAudit(ctx, tx, a, key, "manual-run", p.Revision); e != nil {
		return out, e
	}
	if e = scheduledActor(ctx, tx, a); e != nil {
		return out, e
	}
	out, e = s.scheduledRunView(ctx, tx, r, now)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) ScheduledTaskRuns(ctx context.Context, a MerchantCredentialActor, key string, before int64, limit int) (ScheduledTaskRunsPage, error) {
	out := ScheduledTaskRunsPage{Items: []ScheduledTaskRunView{}}
	if _, ok := scheduledDefinition(key); !ok {
		return out, ErrNotFound
	}
	if before < 0 || limit < 1 || limit > 100 {
		return out, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = scheduledActor(ctx, tx, a); e != nil {
		return out, e
	}
	now, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM scheduled_task_runs WHERE task_key=$1 AND ($2::bigint=0 OR id<$2)`, key, before).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.Query(ctx, `SELECT id FROM scheduled_task_runs WHERE task_key=$1 AND ($2::bigint=0 OR id<$2)ORDER BY id DESC LIMIT $3`, key, before, limit)
	if e != nil {
		return out, e
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for _, id := range ids {
		r, e := s.readScheduledRun(ctx, tx, id)
		if e != nil {
			return out, e
		}
		v, e := s.scheduledRunView(ctx, tx, r, now)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	if len(ids) == limit {
		out.NextCursor = ids[len(ids)-1]
	}
	if e = scheduledActor(ctx, tx, a); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
