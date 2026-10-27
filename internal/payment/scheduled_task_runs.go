package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type scheduledSettlementSlot struct{ PlanID, Revision, Slot, AutoAuthorizationID int64 }
type scheduledTaskFacts struct {
	Policy                  scheduledTaskPolicy
	Trigger                 string
	Slot                    int64
	ScheduledFor, CreatedAt time.Time
	BusinessDate            string
	Plans                   []scheduledSettlementSlot
	Merchants               []int64
	Jobs                    []scheduledJobFact
}
type scheduledTaskRun struct {
	id             int64
	facts          scheduledTaskFacts
	proof          []byte
	state          string
	token          []byte
	epoch          int64
	until, started *time.Time
	progress       int64
	progressCounts ScheduledTaskCounts
	progressProof  []byte
}

func (s *Store) scheduledIntentProof(id int64, f scheduledTaskFacts) ([]byte, error) {
	return s.referralDigest("scheduler171/intent", []any{id, f})
}
func (s *Store) insertScheduledRun(ctx context.Context, tx pgx.Tx, p *scheduledTaskPolicy, trigger string, slot int64, at, now time.Time) (scheduledTaskRun, error) {
	r := scheduledTaskRun{state: "pending", facts: scheduledTaskFacts{Policy: *p, Trigger: trigger, Slot: slot, ScheduledFor: at.UTC(), CreatedAt: now.UTC(), BusinessDate: now.In(settlementScheduleZone).Format("2006-01-02"), Plans: []scheduledSettlementSlot{}, Merchants: []int64{}}}
	var e error
	if p.Task == "notify" || p.Task == "order_query" || p.Task == "profitsharing" {
		r.facts.Jobs, e = s.scheduledJobSnapshot(ctx, tx, *p, now)
		if e != nil {
			return r, e
		}
	}
	if p.Task == "settlement_generate" {
		r.facts.Plans, e = s.scheduledSettlementSnapshot(ctx, tx, p.Params["limit"], now)
		if e != nil {
			return r, e
		}
	}
	if p.Task == "transfer" || p.Task == "settle" {
		r.facts.Plans, e = s.scheduledSettlementSnapshotFor(ctx, tx, p.Task, 20, now)
		if e != nil {
			return r, e
		}
	}
	if p.Task == "daily_report" || p.Task == "balance_notice" || p.Task == "order" {
		r.facts.Merchants, e = settlementPlanIDs(ctx, tx, `SELECT id FROM merchants WHERE enabled ORDER BY id LIMIT 10001`)
		if e != nil {
			return r, e
		}
		if len(r.facts.Merchants) > 10000 {
			return r, ErrState
		}
	}
	if e = tx.QueryRow(ctx, `SELECT nextval('scheduled_task_runs_id_seq')`).Scan(&r.id); e != nil {
		return r, e
	}
	r.proof, e = s.scheduledIntentProof(r.id, r.facts)
	if e != nil {
		return r, e
	}
	r.progressProof, e = s.scheduledProgressProof(r, 0, ScheduledTaskCounts{})
	if e != nil {
		return r, e
	}
	counts, e := json.Marshal(ScheduledTaskCounts{})
	if e != nil {
		return r, e
	}
	raw, e := json.Marshal(r.facts)
	if e != nil {
		return r, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO scheduled_task_runs(id,task_key,generation,trigger_kind,slot_no,scheduled_for,facts,source_hash,created_at,progress_counts,progress_hash)VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10::jsonb,$11)`, r.id, p.Task, p.Generation, trigger, slot, at, string(raw), r.proof, now, string(counts), r.progressProof)
	if e == nil {
		_, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'task-scheduler','scheduled-task-runs',$1,'intent',jsonb_build_object('task',$2::text,'trigger',$3::text,'generation',$4::text,'slot',$5::text,'intent_hash',$6::text))`, r.id, p.Task, trigger, fmt.Sprint(p.Generation), fmt.Sprint(slot), hex.EncodeToString(r.proof))
	}
	if e == nil {
		p.LastRunID = r.id
		if trigger == "schedule" {
			p.AutoSlot = slot
		}
	}
	return r, e
}
func (s *Store) readScheduledRun(ctx context.Context, q referralReader, id int64) (scheduledTaskRun, error) {
	r := scheduledTaskRun{id: id}
	var raw, progressRaw []byte
	var key, trigger string
	var generation, slot int64
	var at, created time.Time
	e := q.QueryRow(ctx, `SELECT task_key,generation,trigger_kind,slot_no,scheduled_for,facts,source_hash,created_at,state,lease_token,lease_epoch,lease_until,started_at,progress_index,progress_counts,progress_hash FROM scheduled_task_runs WHERE id=$1`, id).Scan(&key, &generation, &trigger, &slot, &at, &raw, &r.proof, &created, &r.state, &r.token, &r.epoch, &r.until, &r.started, &r.progress, &progressRaw, &r.progressProof)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	if r.started != nil {
		x := r.started.UTC()
		r.started = &x
	}
	if r.until != nil {
		x := r.until.UTC()
		r.until = &x
	}
	if decodeScheduledJSON(raw, &r.facts) != nil {
		return r, ErrConflict
	}
	f := r.facts
	expected, e := s.scheduledIntentProof(id, f)
	configProof, ce := s.scheduledPolicyProof(f.Policy)
	if e != nil || ce != nil || !hmac.Equal(expected, r.proof) || !hmac.Equal(configProof, f.Policy.Proof) || f.Policy.Task != key || f.Policy.Generation != generation || f.Trigger != trigger || f.Slot != slot || !f.ScheduledFor.Equal(at) || !f.CreatedAt.Equal(created) || f.Policy.Schedule.validate() != nil || !validScheduledParams(key, f.Policy.Params) || !f.Policy.Enabled || f.BusinessDate != f.CreatedAt.In(settlementScheduleZone).Format("2006-01-02") {
		return r, ErrConflict
	}
	if d, ok := scheduledDefinition(key); !ok || d.unavailable != "" {
		return r, ErrConflict
	}
	if decodeScheduledJSON(progressRaw, &r.progressCounts) != nil || r.progress < 0 || r.progress > int64(len(f.Merchants)) {
		return r, ErrConflict
	}
	progressProof, e := s.scheduledProgressProof(r, r.progress, r.progressCounts)
	if e != nil || !hmac.Equal(progressProof, r.progressProof) {
		return r, ErrConflict
	}
	var origin, result bool
	e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='scheduled-task-runs' AND resource_id=$1 AND operation='intent' AND actor_role='system' AND actor_id=0 AND actor_name='task-scheduler' AND changes->>'task'=$2 AND changes->>'trigger'=$3 AND changes->>'generation'=$4 AND changes->>'slot'=$5 AND changes->>'intent_hash'=$6),EXISTS(SELECT 1 FROM scheduled_task_results WHERE run_id=$1)`, r.id, key, trigger, fmt.Sprint(generation), fmt.Sprint(slot), hex.EncodeToString(r.proof)).Scan(&origin, &result)
	if e != nil {
		return r, e
	}
	if !origin || result != (r.state == "completed") {
		return r, ErrConflict
	}
	if e = s.verifyScheduledExecutionAnchor(ctx, q, r); e != nil {
		return r, e
	}
	return r, nil
}

func (s *Store) scheduledProgressProof(r scheduledTaskRun, index int64, counts ScheduledTaskCounts) ([]byte, error) {
	return s.referralDigest("scheduler171/progress", []any{r.id, r.proof, index, counts})
}
func (s *Store) checkpointScheduledRun(ctx context.Context, r scheduledTaskRun, index int64, counts ScheduledTaskCounts) error {
	if index < r.progress || index > int64(len(r.facts.Merchants)) {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var valid bool
	var until time.Time
	if e = tx.QueryRow(ctx, `SELECT state='running' AND lease_token=$2 AND lease_epoch=$3 AND lease_until>clock_timestamp(),lease_until FROM scheduled_task_runs WHERE id=$1 FOR UPDATE`, r.id, r.token, r.epoch).Scan(&valid, &until); e != nil {
		return e
	}
	if !valid {
		return ErrConflict
	}
	proof, e := s.scheduledProgressProof(r, index, counts)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(counts)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE scheduled_task_runs SET progress_index=$2,progress_counts=$3::jsonb,progress_hash=$4 WHERE id=$1`, r.id, index, string(raw), proof); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'task-scheduler','scheduled-task-execution',$1,'checkpoint',jsonb_build_object('index',$2::text,'progress_hash',$3::text))`, r.id, fmt.Sprint(index), hex.EncodeToString(proof)); e != nil {
		return e
	}
	now, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return e
	}
	if !now.Before(until) {
		return ErrConflict
	}
	return tx.Commit(ctx)
}
func (s *Store) scheduledResultProof(r scheduledTaskRun, status string, counts ScheduledTaskCounts, code string, at time.Time) ([]byte, error) {
	return s.referralDigest("scheduler171/result", []any{r.id, r.proof, r.epoch, r.started, status, counts, code, at.UTC().Format(time.RFC3339Nano)})
}
func (s *Store) scheduledRunView(ctx context.Context, q referralReader, r scheduledTaskRun, now time.Time) (ScheduledTaskRunView, error) {
	v := ScheduledTaskRunView{ID: r.id, Task: r.facts.Policy.Task, Trigger: r.facts.Trigger, Status: r.state, ScheduledFor: r.facts.ScheduledFor, CreatedAt: r.facts.CreatedAt, StartedAt: r.started}
	if r.state == "running" && r.until != nil && !now.Before(*r.until) {
		v.Status = "lease_expired"
	}
	var countsRaw, proof []byte
	var status, code string
	var completed time.Time
	e := q.QueryRow(ctx, `SELECT status,counts,error_code,completed_at,proof_hash FROM scheduled_task_results WHERE run_id=$1`, r.id).Scan(&status, &countsRaw, &code, &completed, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		if r.state == "completed" {
			return v, ErrConflict
		}
		return v, nil
	}
	if e != nil {
		return v, e
	}
	if r.state != "completed" {
		return v, ErrConflict
	}
	var counts ScheduledTaskCounts
	if decodeScheduledJSON(countsRaw, &counts) != nil || counts.Examined < 0 || counts.Awarded < 0 || counts.Generated < 0 || counts.Skipped < 0 {
		return v, ErrConflict
	}
	expected, e := s.scheduledResultProof(r, status, counts, code, completed)
	if e != nil || !hmac.Equal(expected, proof) {
		return v, ErrConflict
	}
	v.Status = status
	completed = completed.UTC()
	v.CompletedAt = &completed
	v.Counts = &counts
	v.ErrorCode = code
	return v, nil
}
func (s *Store) finishScheduledRunTx(ctx context.Context, tx pgx.Tx, r scheduledTaskRun, status string, counts ScheduledTaskCounts, code string, at time.Time) error {
	proof, e := s.scheduledResultProof(r, status, counts, code, at)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(counts)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO scheduled_task_results(run_id,status,counts,error_code,completed_at,proof_hash)VALUES($1,$2,$3::jsonb,$4,$5,$6)`, r.id, status, string(raw), code, at, proof); e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `UPDATE scheduled_task_runs SET state='completed',lease_token=NULL,lease_until=NULL WHERE id=$1 AND state<>'completed'`, r.id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
func (s *Store) claimScheduledRun(ctx context.Context, conn *pgxpool.Conn, key string, lease time.Duration) (scheduledTaskRun, bool, error) {
	var empty scheduledTaskRun
	tx, e := conn.Begin(ctx)
	if e != nil {
		return empty, false, e
	}
	defer tx.Rollback(ctx)
	p, configured, e := s.scheduledPolicy(ctx, tx, key, true)
	if e != nil {
		return empty, false, e
	}
	if !configured || !p.Enabled {
		return empty, false, nil
	}
	now, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return empty, false, e
	}
	var id int64
	e = tx.QueryRow(ctx, `SELECT id FROM scheduled_task_runs WHERE task_key=$1 AND (state='pending' OR state='running' AND lease_until<=clock_timestamp()) ORDER BY id LIMIT 1 FOR UPDATE`, key).Scan(&id)
	var r scheduledTaskRun
	if errors.Is(e, pgx.ErrNoRows) {
		slot, at, de := p.Schedule.due(now, p.AutoSlot)
		if errors.Is(de, ErrState) {
			return empty, false, nil
		}
		if de != nil {
			return empty, false, de
		}
		r, e = s.insertScheduledRun(ctx, tx, &p, "schedule", slot, at, now)
		if e != nil {
			return empty, false, e
		}
		if e = s.saveScheduledPolicy(ctx, tx, &p); e != nil {
			return empty, false, e
		}
	} else if e != nil {
		return empty, false, e
	} else {
		r, e = s.readScheduledRun(ctx, tx, id)
		if e != nil {
			return empty, false, e
		}
		if r.facts.Policy.Generation != p.Generation {
			return empty, false, ErrConflict
		}
	}
	token := make([]byte, 32)
	if _, e = rand.Read(token); e != nil {
		return empty, false, e
	}
	tag, e := tx.Exec(ctx, `UPDATE scheduled_task_runs SET state='running',lease_token=$2,lease_epoch=lease_epoch+1,lease_until=clock_timestamp()+$3::bigint*interval '1 millisecond',started_at=COALESCE(started_at,clock_timestamp())WHERE id=$1 AND (state='pending' OR state='running' AND lease_until<=clock_timestamp())`, r.id, token, lease.Milliseconds())
	if e != nil {
		return empty, false, e
	}
	if tag.RowsAffected() != 1 {
		return empty, false, ErrConflict
	}
	var epoch int64
	var started time.Time
	if e = tx.QueryRow(ctx, `SELECT lease_epoch,started_at FROM scheduled_task_runs WHERE id=$1`, r.id).Scan(&epoch, &started); e != nil {
		return empty, false, e
	}
	tokenHash := sha256.Sum256(token)
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'task-scheduler','scheduled-task-execution',$1,'lease',jsonb_build_object('epoch',$2::text,'token_hash',$3::text,'started_at',$4::text))`, r.id, fmt.Sprint(epoch), hex.EncodeToString(tokenHash[:]), started.UTC().Format(time.RFC3339Nano)); e != nil {
		return empty, false, e
	}
	r, e = s.readScheduledRun(ctx, tx, r.id)
	if e != nil {
		return empty, false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, false, e
	}
	return r, true, nil
}
func (s *Store) renewScheduledLease(ctx context.Context, r scheduledTaskRun, lease time.Duration) error {
	tag, e := s.pool.Exec(ctx, `UPDATE scheduled_task_runs SET lease_until=clock_timestamp()+$4::bigint*interval '1 millisecond' WHERE id=$1 AND state='running' AND lease_token=$2 AND lease_epoch=$3 AND lease_until>clock_timestamp()`, r.id, r.token, r.epoch, lease.Milliseconds())
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
func (s *Store) completeScheduledLease(ctx context.Context, r scheduledTaskRun, counts ScheduledTaskCounts, handlerError error) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var valid bool
	var leaseUntil time.Time
	if e = tx.QueryRow(ctx, `SELECT state='running' AND lease_token=$2 AND lease_epoch=$3 AND lease_until>clock_timestamp(),lease_until FROM scheduled_task_runs WHERE id=$1 FOR UPDATE`, r.id, r.token, r.epoch).Scan(&valid, &leaseUntil); e != nil {
		return e
	}
	if !valid {
		return ErrConflict
	}
	now, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return e
	}
	status, code := "success", ""
	if handlerError != nil {
		status, code = "failed", "execution_failed"
		if errors.Is(handlerError, ErrState) || errors.Is(handlerError, ErrConflict) || errors.Is(handlerError, ErrInvalid) {
			code = "business_state_changed"
		}
	}
	if e = s.finishScheduledRunTx(ctx, tx, r, status, counts, code, now); e != nil {
		return e
	}
	finalTime, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return e
	}
	if !finalTime.Before(leaseUntil) {
		return ErrConflict
	}
	return tx.Commit(ctx)
}

type ScheduledTaskTick struct{ Claimed, Completed, Failed int }

func (s *Store) runScheduledTask(ctx context.Context, key string, lease, timeout time.Duration) (bool, bool, error) {
	if lease < 100*time.Millisecond || lease > 5*time.Minute || timeout < lease {
		return false, false, ErrInvalid
	}
	conn, e := s.pool.Acquire(ctx)
	if e != nil {
		return false, false, e
	}
	lockKey := "goepay/scheduled-task/" + key
	var locked bool
	if e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, lockKey).Scan(&locked); e != nil {
		conn.Release()
		return false, false, e
	}
	if !locked {
		conn.Release()
		return false, false, nil
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lockKey); err != nil {
			raw := conn.Hijack()
			raw.Close(cleanup)
		} else {
			conn.Release()
		}
	}()
	r, claimed, e := s.claimScheduledRun(ctx, conn, key, lease)
	if e != nil || !claimed {
		return false, false, e
	}
	work, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		counts ScheduledTaskCounts
		err    error
	}
	done := make(chan result, 1)
	go func() { c, e := s.executeScheduledTask(work, r); done <- result{c, e} }()
	renew := time.NewTicker(lease / 3)
	defer renew.Stop()
	for {
		select {
		case value := <-done:
			if ctx.Err() != nil {
				return true, false, ctx.Err()
			}
			finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
			e = s.completeScheduledLease(finish, r, value.counts, value.err)
			stop()
			return true, value.err != nil, e
		case <-renew.C:
			renewCtx, stop := context.WithTimeout(work, lease/3)
			e = s.renewScheduledLease(renewCtx, r, lease)
			stop()
			if e != nil {
				cancel()
				<-done
				return true, false, e
			}
		case <-work.Done():
			cancel()
			<-done
			return true, false, work.Err()
		}
	}
}

// RunScheduledTasksOnce has no provider client and never sends a payment,
// notification or automatic payout. Only immutable original local facts run.
func (s *Store) RunScheduledTasksOnce(ctx context.Context, limit int) (ScheduledTaskTick, error) {
	var tick ScheduledTaskTick
	if limit < 1 || limit > 100 {
		return tick, ErrInvalid
	}
	for _, d := range scheduledTaskRegistry {
		if d.unavailable != "" {
			continue
		}
		claimed, failed, e := s.runScheduledTask(ctx, d.key, time.Minute, 5*time.Minute)
		if e != nil {
			return tick, e
		}
		if claimed {
			tick.Claimed++
			if failed {
				tick.Failed++
			} else {
				tick.Completed++
			}
		}
		if tick.Claimed >= limit {
			break
		}
	}
	return tick, nil
}
