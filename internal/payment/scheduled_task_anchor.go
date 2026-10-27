package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// An old correctly MACed row is not the current committed configuration. Config
// and manual requests both advance revision; only configuration advances the
// generation. Automatic slots advance neither and have an independent original
// intent audit/head. These anchors never infer execution from an updated_at.
func (s *Store) verifyScheduledPolicyAnchor(ctx context.Context, tx pgx.Tx, p scheduledTaskPolicy) error {
	var commits, generations int64
	var ordered bool
	e := tx.QueryRow(ctx, `SELECT count(*),count(*)FILTER(WHERE operation='configure'),COALESCE(bool_and(changes->>'revision'=ordinal::text),true) FROM(
 SELECT operation,changes,row_number()OVER(ORDER BY id)ordinal FROM backoffice_audit WHERE resource='scheduled-tasks' AND resource_id=0 AND actor_role='admin' AND actor_id>0 AND operation IN('configure','manual-run') AND changes->>'task'=$1)a`, p.Task).Scan(&commits, &generations, &ordered)
	if e != nil {
		return e
	}
	if !ordered || commits != p.Revision || generations != p.Generation {
		return ErrConflict
	}
	var last, manual, slot, originLast, originManual int64
	var originSlot string
	e = tx.QueryRow(ctx, `SELECT
 COALESCE((SELECT id FROM scheduled_task_runs WHERE task_key=$1 ORDER BY id DESC LIMIT 1),0),
 COALESCE((SELECT id FROM scheduled_task_runs WHERE task_key=$1 AND trigger_kind='manual' ORDER BY id DESC LIMIT 1),0),
 COALESCE((SELECT slot_no FROM scheduled_task_runs WHERE task_key=$1 AND generation=$2 AND trigger_kind='schedule' ORDER BY slot_no DESC LIMIT 1),-1),
 COALESCE((SELECT resource_id FROM backoffice_audit WHERE resource='scheduled-task-runs' AND operation='intent' AND actor_role='system' AND actor_id=0 AND actor_name='task-scheduler' AND changes->>'task'=$1 ORDER BY resource_id DESC LIMIT 1),0),
 COALESCE((SELECT resource_id FROM backoffice_audit WHERE resource='scheduled-task-runs' AND operation='intent' AND actor_role='system' AND actor_id=0 AND actor_name='task-scheduler' AND changes->>'task'=$1 AND changes->>'trigger'='manual' ORDER BY resource_id DESC LIMIT 1),0),
 COALESCE((SELECT changes->>'slot' FROM backoffice_audit WHERE resource='scheduled-task-runs' AND operation='intent' AND actor_role='system' AND actor_id=0 AND actor_name='task-scheduler' AND changes->>'task'=$1 AND changes->>'trigger'='schedule' AND changes->>'generation'=$3 ORDER BY resource_id DESC LIMIT 1),'-1')`, p.Task, p.Generation, fmt.Sprint(p.Generation)).Scan(&last, &manual, &slot, &originLast, &originManual, &originSlot)
	if e != nil {
		return e
	}
	if p.LastRunID != last || p.LastManualRunID != manual || p.AutoSlot != slot || last != originLast || manual != originManual || originSlot != fmt.Sprint(slot) {
		return ErrConflict
	}
	if last > 0 {
		r, e := s.readScheduledRun(ctx, tx, last)
		if e != nil {
			return e
		}
		if r.facts.Policy.Task != p.Task {
			return ErrConflict
		}
		now, e := scheduledDBNow(ctx, tx)
		if e != nil {
			return e
		}
		if _, e = s.scheduledRunView(ctx, tx, r, now); e != nil {
			return e
		}
	}
	return nil
}

func (s *Store) verifyScheduledExecutionAnchor(ctx context.Context, q referralReader, r scheduledTaskRun) error {
	var epochs int64
	var ordered bool
	if e := q.QueryRow(ctx, `SELECT count(*),COALESCE(bool_and(changes->>'epoch'=ordinal::text),true)FROM(SELECT changes,row_number()OVER(ORDER BY id)ordinal FROM backoffice_audit WHERE resource='scheduled-task-execution' AND resource_id=$1 AND operation='lease' AND actor_role='system' AND actor_id=0 AND actor_name='task-scheduler')a`, r.id).Scan(&epochs, &ordered); e != nil {
		return e
	}
	if !ordered || epochs != r.epoch || r.state == "pending" && epochs != 0 {
		return ErrConflict
	}
	if r.state == "running" {
		if r.started == nil {
			return ErrConflict
		}
		token := sha256.Sum256(r.token)
		var matches bool
		if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='scheduled-task-execution' AND resource_id=$1 AND operation='lease' AND actor_role='system' AND actor_id=0 AND actor_name='task-scheduler' AND changes->>'epoch'=$2 AND changes->>'token_hash'=$3 AND changes->>'started_at'=$4)`, r.id, fmt.Sprint(r.epoch), hex.EncodeToString(token[:]), r.started.UTC().Format(time.RFC3339Nano)).Scan(&matches); e != nil {
			return e
		}
		if !matches {
			return ErrConflict
		}
	}
	var index, proof string
	e := q.QueryRow(ctx, `SELECT changes->>'index',changes->>'progress_hash' FROM backoffice_audit WHERE resource='scheduled-task-execution' AND resource_id=$1 AND operation='checkpoint' AND actor_role='system' AND actor_id=0 AND actor_name='task-scheduler' ORDER BY id DESC LIMIT 1`, r.id).Scan(&index, &proof)
	if e == pgx.ErrNoRows {
		if r.progress != 0 {
			return ErrConflict
		}
		return nil
	}
	if e != nil {
		return e
	}
	if index != fmt.Sprint(r.progress) || proof != hex.EncodeToString(r.progressProof) {
		return ErrConflict
	}
	return nil
}
