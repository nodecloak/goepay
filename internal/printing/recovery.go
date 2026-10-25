package printing

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

type recoveryRecord struct {
	ID, IntentID, MerchantID, OldTask, NewTask int64
	Sequence                                   int
	Actor                                      string
	At                                         time.Time
	Proof                                      []byte
}

func (s *Store) recoveryProof(r recoveryRecord) ([]byte, error) {
	return s.digest("recovery", []any{r.ID, r.IntentID, r.MerchantID, r.OldTask, r.NewTask, r.Sequence, r.Actor, r.At.UTC().Format(time.RFC3339Nano)})
}
func printingTask(ctx context.Context, q reader, id, intent int64, event string) (string, error) {
	var kind, key, state string
	var raw []byte
	e := q.QueryRow(ctx, "SELECT kind,event_key,payload,state FROM jobs WHERE id=$1", id).Scan(&kind, &key, &raw, &state)
	if e != nil {
		return "", e
	}
	var fields map[string]string
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 1 || fields["intent_id"] != fmt.Sprint(intent) || kind != "receipt-print.send" || key != event {
		return "", ErrConflict
	}
	return state, nil
}
func (s *Store) attachTask(ctx context.Context, q reader, r *intentRecord) error {
	var original int64
	e := q.QueryRow(ctx, "SELECT id FROM jobs WHERE event_key=$1", fmt.Sprintf("receipt-intent:%d", r.V.ID)).Scan(&original)
	if errors.Is(e, pgx.ErrNoRows) {
		r.V.TaskState = "missing"
		r.V.State = "blocked"
		r.V.Reason = "task_missing"
		return nil
	}
	if e != nil {
		return e
	}
	state, e := printingTask(ctx, q, original, r.V.ID, fmt.Sprintf("receipt-intent:%d", r.V.ID))
	if e != nil {
		return e
	}
	rows, e := q.Query(ctx, `SELECT id,intent_id,merchant_id,expected_task_id,new_task_id,sequence_no,actor_name,created_at,proof_hash FROM printing_recoveries WHERE intent_id=$1 ORDER BY sequence_no LIMIT 6`, r.V.ID)
	if e != nil {
		return e
	}
	var history []recoveryRecord
	for rows.Next() {
		var x recoveryRecord
		if e = rows.Scan(&x.ID, &x.IntentID, &x.MerchantID, &x.OldTask, &x.NewTask, &x.Sequence, &x.Actor, &x.At, &x.Proof); e != nil {
			rows.Close()
			return e
		}
		history = append(history, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(history) > 5 {
		return ErrConflict
	}
	current := original
	for n, x := range history {
		want, e := s.recoveryProof(x)
		if e != nil || !hmac.Equal(want, x.Proof) || x.Sequence != n+1 || x.MerchantID != r.V.MerchantID || x.OldTask != current {
			return ErrConflict
		}
		var audited bool
		if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='printing-recoveries' AND resource_id=$1 AND operation='recover' AND actor_role='merchant' AND actor_id=$2 AND actor_name=$3)`, x.ID, x.MerchantID, x.Actor).Scan(&audited); e != nil {
			return e
		}
		if !audited {
			return ErrConflict
		}
		oldState, e := printingTask(ctx, q, x.OldTask, r.V.ID, taskEvent(r.V.ID, history, n-1))
		if e != nil || oldState != "dead" {
			return ErrConflict
		}
		state, e = printingTask(ctx, q, x.NewTask, r.V.ID, fmt.Sprintf("receipt-intent:%d:recovery:%d", r.V.ID, x.ID))
		if e != nil {
			return e
		}
		current = x.NewTask
	}
	r.V.TaskID = current
	r.V.TaskState = state
	switch state {
	case "pending", "running":
	case "dead":
		r.V.State = "blocked"
		r.V.Reason = "task_dead"
		r.V.CanRecover = len(history) < 5
	default:
		r.V.State = "blocked"
		r.V.Reason = "task_unconfirmed"
	}
	return nil
}
func taskEvent(id int64, h []recoveryRecord, n int) string {
	if n < 0 {
		return fmt.Sprintf("receipt-intent:%d", id)
	}
	return fmt.Sprintf("receipt-intent:%d:recovery:%d", id, h[n].ID)
}

// Recover creates one new queue task for an unclaimed intent. Replaying an old
// expected task returns its original replacement and consumes no recovery budget.
func (s *Store) Recover(ctx context.Context, mid, id, expectedTaskID int64, a Actor) (JobView, error) {
	var out JobView
	if !owner(mid, a) || id <= 0 || expectedTaskID <= 0 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	var locked int64
	if e = tx.QueryRow(ctx, "SELECT id FROM merchants WHERE id=$1 FOR NO KEY UPDATE", mid).Scan(&locked); e != nil {
		return out, e
	}
	r, e := s.readJob(ctx, tx, mid, id)
	if e != nil {
		return out, e
	}
	var replacement int64
	e = tx.QueryRow(ctx, "SELECT new_task_id FROM printing_recoveries WHERE intent_id=$1 AND expected_task_id=$2", id, expectedTaskID).Scan(&replacement)
	if e == nil {
		r.V.TaskID = replacement
		if e = tx.QueryRow(ctx, "SELECT state FROM jobs WHERE id=$1", replacement).Scan(&r.V.TaskState); e != nil {
			return out, e
		}
		r.V.CanRecover = false
		return r.V, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if r.V.TaskID != expectedTaskID || !r.V.CanRecover || r.V.State != "blocked" || r.V.Reason != "task_dead" {
		return out, ErrState
	}
	// Lock the expected dead row before allocating an immutable replacement.
	var taskState string
	if e = tx.QueryRow(ctx, "SELECT state FROM jobs WHERE id=$1 FOR UPDATE", expectedTaskID).Scan(&taskState); e != nil {
		return out, e
	}
	if taskState != "dead" {
		return out, ErrConflict
	}
	x := recoveryRecord{IntentID: id, MerchantID: mid, OldTask: expectedTaskID, Actor: a.Name}
	if e = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('printing_recoveries','id')),nextval(pg_get_serial_sequence('jobs','id')),1+(SELECT count(*) FROM printing_recoveries WHERE intent_id=$1),transaction_timestamp()`, id).Scan(&x.ID, &x.NewTask, &x.Sequence, &x.At); e != nil {
		return out, e
	}
	if x.Sequence > 5 {
		return out, ErrState
	}
	x.Proof, e = s.recoveryProof(x)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO jobs(id,event_key,kind,payload) VALUES($1,$2,'receipt-print.send',jsonb_build_object('intent_id',$3::bigint::text))`, x.NewTask, fmt.Sprintf("receipt-intent:%d:recovery:%d", id, x.ID), id); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO printing_recoveries(id,intent_id,merchant_id,expected_task_id,new_task_id,sequence_no,actor_name,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, x.ID, id, mid, expectedTaskID, x.NewTask, x.Sequence, a.Name, x.Proof, x.At); e != nil {
		return out, e
	}
	if e = audit(ctx, tx, a, "printing-recoveries", x.ID, "recover", map[string]string{"intent_id": fmt.Sprint(id), "expected_task_id": fmt.Sprint(expectedTaskID)}); e != nil {
		return out, e
	}
	r, e = s.readJob(ctx, tx, mid, id)
	if e != nil {
		return out, e
	}
	return r.V, tx.Commit(ctx)
}
