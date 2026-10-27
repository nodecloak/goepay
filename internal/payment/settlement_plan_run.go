package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type settlementPlanRunRecord struct {
	View      SettlementPlanRunView
	Proof     []byte
	selection *settlementThresholdSelection
}
type settlementPlanItemRecord struct {
	View      SettlementPlanItemView
	RunID     int64
	CreatedAt time.Time
	Proof     []byte
}

func settlementPlanAuthorityAt(a SettlementPlanAuthorizationView, at time.Time) string {
	if a.Revoked {
		return "authorization_revoked"
	}
	if at.Before(a.ValidFrom) {
		return "authorization_not_started"
	}
	if !at.Before(a.ExpiresAt) {
		return "authorization_expired"
	}
	return ""
}

func dueSettlementSlot(spec SettlementScheduleSpec, now time.Time) (int64, time.Time, error) {
	_, first, err := nextSettlementSlot(spec, -1)
	if err != nil {
		return 0, time.Time{}, err
	}
	if now.Before(first) {
		return 0, time.Time{}, ErrState
	}
	if spec.Frequency == "once" {
		return 0, first, nil
	}
	unit := int64(1)
	switch spec.Frequency {
	case "minute":
		unit = 60
	case "hour":
		unit = 3600
	case "day":
		unit = 86400
	}
	slot := (now.Unix() - first.Unix()) / (spec.Interval * unit)
	return nextSettlementSlot(spec, slot-1)
}

func (s *Store) settlementPlanRunProof(r settlementPlanRunRecord) ([]byte, error) {
	v := r.View
	return s.referralDigest("settlement-plan-run", []any{v.ID, v.PlanID, v.PlanRevision, v.AuthorizationID, v.SlotNo, v.BusinessDate, v.ScheduledAt.UTC().Format(time.RFC3339Nano), v.CreatedAt.UTC().Format(time.RFC3339Nano)})
}

func (s *Store) readSettlementPlanRun(ctx context.Context, q fundsReader, id int64) (settlementPlanRunRecord, error) {
	var r settlementPlanRunRecord
	var audited bool
	err := q.QueryRow(ctx, `SELECT r.id,r.plan_id,r.plan_revision,r.authorization_id,r.slot_no,r.business_date::text,r.scheduled_at,r.created_at,r.proof_hash,
 EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-plan-runs' AND b.resource_id=r.id AND b.operation='schedule' AND b.actor_role='system' AND b.actor_id=0 AND b.actor_name='settlement-scheduler') FROM settlement_plan_runs r WHERE r.id=$1`, id).Scan(&r.View.ID, &r.View.PlanID, &r.View.PlanRevision, &r.View.AuthorizationID, &r.View.SlotNo, &r.View.BusinessDate, &r.View.ScheduledAt, &r.View.CreatedAt, &r.Proof, &audited)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	proof, err := s.settlementPlanRunProof(r)
	if err != nil || !hmac.Equal(proof, r.Proof) || !audited {
		return r, ErrConflict
	}
	a, err := s.readSettlementPlanAuthorization(ctx, q, r.View.AuthorizationID)
	if err != nil {
		return r, err
	}
	if a.View.PlanID != r.View.PlanID || a.View.PlanRevision != r.View.PlanRevision {
		return r, ErrConflict
	}
	d, err := s.readSettlementPlanDefinition(ctx, q, a.Scope.DefinitionID)
	if err != nil {
		return r, err
	}
	slot, at, err := nextSettlementSlot(d.Input.Schedule, r.View.SlotNo-1)
	if err != nil || slot != r.View.SlotNo || !at.Equal(r.View.ScheduledAt) {
		return r, ErrConflict
	}
	date, err := settlementBusinessDate(r.View.ScheduledAt, 0)
	if err != nil || date != r.View.BusinessDate {
		return r, ErrConflict
	}
	if r.selection, err = s.verifyThresholdRunSource(ctx, q, r, d.Input.Mode); err != nil {
		return r, err
	}
	return r, nil
}

func (s *Store) createSettlementPlanRun(ctx context.Context, id, expected int64, recoveryActor *SettlementPlanActor) (SettlementPlanRunView, error) {
	var out SettlementPlanRunView
	if recoveryActor != nil && !settlementPlanAdminValid(*recoveryActor) {
		return out, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, "SELECT id FROM settlement_plans WHERE id=$1 FOR UPDATE", id).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	} else if err != nil {
		return out, err
	}
	p, err := s.readSettlementPlan(ctx, tx, id)
	if err != nil {
		return out, err
	}
	if p.View.Revision != expected {
		return out, ErrConflict
	}
	if !p.View.Enabled || p.View.LastAuthorizationID <= 0 {
		return out, ErrState
	}
	a, err := s.readSettlementPlanAuthorization(ctx, tx, p.View.LastAuthorizationID)
	if err != nil {
		return out, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return out, err
	}
	if settlementPlanAuthorityAt(a.View, now) != "" {
		return out, ErrState
	}
	scope, err := s.settlementPlanStaticScope(ctx, tx, p)
	if err != nil {
		return out, err
	}
	x, _ := json.Marshal(scope)
	y, _ := json.Marshal(a.Scope)
	if !bytes.Equal(x, y) {
		return out, ErrConflict
	}
	slot, at, err := dueSettlementSlot(p.Input.Schedule, now)
	if err != nil {
		return out, err
	}
	if original, ok := ctx.Value(scheduledTaskSettlementSlotKey{}).(scheduledTaskSettlementExpected); ok && (original.PlanID != id || original.Slot != slot) {
		return out, ErrState
	}
	day, err := settlementBusinessDate(now, 0)
	if err != nil {
		return out, err
	}
	scheduledDay, err := settlementBusinessDate(at, 0)
	if err != nil || scheduledDay != day {
		return out, ErrState
	}
	var old int64
	err = tx.QueryRow(ctx, "SELECT id FROM settlement_plan_runs WHERE plan_id=$1 AND slot_no=$2", id, slot).Scan(&old)
	if err == nil {
		r, e := s.readSettlementPlanRun(ctx, tx, old)
		if e != nil {
			return out, e
		}
		// A replacement authority must not re-arm a slot whose immutable run
		// belongs to the previous authority, even when the revision is unchanged.
		if r.View.AuthorizationID != a.View.ID || r.View.PlanRevision != expected {
			return out, ErrConflict
		}
		key := fmt.Sprintf("settlement-plan-run:%d", r.View.ID)
		var jobID int64
		var kind, state string
		var attempts int
		var matchingPayload bool
		e = tx.QueryRow(ctx, `SELECT id,kind,state,attempts,payload=jsonb_build_object('run_id',$2::text) FROM jobs WHERE event_key=$1 FOR UPDATE`, key, fmt.Sprint(r.View.ID)).Scan(&jobID, &kind, &state, &attempts, &matchingPayload)
		if errors.Is(e, pgx.ErrNoRows) {
			_, e = tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload) VALUES($1,'settlement-plan.generate',jsonb_build_object('run_id',$2::text))`, key, fmt.Sprint(r.View.ID))
		} else if e == nil {
			if kind != "settlement-plan.generate" || !matchingPayload {
				return out, ErrConflict
			}
			if state == "dead" && recoveryActor != nil {
				// Job lock acquisition can wait beyond the original window or day.
				// Automatic scans never reset an exhausted job's retry budget.
				if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
					return out, e
				}
				day, e = settlementBusinessDate(now, 0)
				if e != nil || day != r.View.BusinessDate || settlementPlanAuthorityAt(a.View, now) != "" {
					return out, ErrState
				}
				scope, e = s.settlementPlanStaticScope(ctx, tx, p)
				if e != nil {
					return out, e
				}
				x, _ = json.Marshal(scope)
				if !bytes.Equal(x, y) {
					return out, ErrConflict
				}
				if _, e = tx.Exec(ctx, `UPDATE jobs SET state='pending',attempts=0,run_at=clock_timestamp(),lease_until=NULL,lease_token=NULL,last_error='' WHERE id=$1 AND state='dead'`, jobID); e != nil {
					return out, e
				}
				e = settlementPlanAudit(ctx, tx, "admin", recoveryActor.ID, recoveryActor.Name, "settlement-plan-runs", r.View.ID, "recover", map[string]string{"job_id": fmt.Sprint(jobID), "plan_id": fmt.Sprint(id), "authorization_id": fmt.Sprint(a.View.ID), "previous_attempts": fmt.Sprint(attempts), "mode": p.Input.Mode})
			}
		}
		if e != nil {
			return out, e
		}
		return r.View, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	r := settlementPlanRunRecord{View: SettlementPlanRunView{PlanID: id, PlanRevision: expected, AuthorizationID: a.View.ID, SlotNo: slot, BusinessDate: day, ScheduledAt: at, State: "pending"}}
	if err = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('settlement_plan_runs','id')),transaction_timestamp()").Scan(&r.View.ID, &r.View.CreatedAt); err != nil {
		return out, err
	}
	r.Proof, err = s.settlementPlanRunProof(r)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO settlement_plan_runs(id,plan_id,plan_revision,authorization_id,slot_no,business_date,scheduled_at,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6::date,$7,$8,$9)`, r.View.ID, id, expected, a.View.ID, slot, day, at, r.Proof, r.View.CreatedAt); err != nil {
		return out, err
	}
	if err = settlementPlanAudit(ctx, tx, "system", 0, "settlement-scheduler", "settlement-plan-runs", r.View.ID, "schedule", map[string]string{"plan_id": fmt.Sprint(id), "mode": p.Input.Mode}); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload) VALUES($1,'settlement-plan.generate',jsonb_build_object('run_id',$2::text))`, fmt.Sprintf("settlement-plan-run:%d", r.View.ID), fmt.Sprint(r.View.ID)); err != nil {
		return out, err
	}
	return r.View, tx.Commit(ctx)
}

func (s *Store) CreateSettlementPlanRun(ctx context.Context, id, expected int64, actor SettlementPlanActor) (SettlementPlanRunView, error) {
	if !settlementPlanAdminValid(actor) || id <= 0 || expected <= 0 {
		return SettlementPlanRunView{}, ErrInvalid
	}
	return s.createSettlementPlanRun(ctx, id, expected, &actor)
}

func (s *Store) settlementPlanItemProof(r settlementPlanItemRecord) ([]byte, error) {
	return s.referralDigest("settlement-plan-item", []any{r.View, r.RunID, r.CreatedAt.UTC().Format(time.RFC3339Nano)})
}

func (s *Store) readSettlementPlanItem(ctx context.Context, q fundsReader, id int64) (settlementPlanItemRecord, error) {
	var r settlementPlanItemRecord
	var valid bool
	err := q.QueryRow(ctx, `SELECT i.id,i.run_id,i.merchant_id,COALESCE(i.request_id,0),i.state,i.reason,i.ledger_available,i.eligible,i.retained,i.gross,i.fee,i.net,COALESCE(i.recipient_config_id,0),i.created_at,i.proof_hash,EXISTS(SELECT 1 FROM settlement_plan_item_invariants v WHERE v.id=i.id AND v.valid IS TRUE) FROM settlement_plan_items i WHERE i.id=$1`, id).Scan(&r.View.ID, &r.RunID, &r.View.MerchantID, &r.View.RequestID, &r.View.State, &r.View.Reason, &r.View.LedgerAvailable, &r.View.Eligible, &r.View.Retained, &r.View.Gross, &r.View.Fee, &r.View.Net, &r.View.RecipientConfigID, &r.CreatedAt, &r.Proof, &valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	proof, err := s.settlementPlanItemProof(r)
	if err != nil || !hmac.Equal(proof, r.Proof) || !valid {
		return r, ErrConflict
	}
	if _, err = s.readSettlementPlanRun(ctx, q, r.RunID); err != nil {
		return r, err
	}
	return r, nil
}

func (s *Store) settlementPlanRunView(ctx context.Context, q referralReader, id int64) (SettlementPlanRunView, error) {
	r, err := s.readSettlementPlanRun(ctx, q, id)
	if err != nil {
		return r.View, err
	}
	ids, err := settlementPlanIDs(ctx, q, "SELECT id FROM settlement_plan_items WHERE run_id=$1 ORDER BY id LIMIT 101", id)
	if err != nil {
		return r.View, err
	}
	if len(ids) > 100 {
		return r.View, ErrConflict
	}
	r.View.Items = []SettlementPlanItemView{}
	for _, iid := range ids {
		item, e := s.readSettlementPlanItem(ctx, q, iid)
		if e != nil {
			return r.View, e
		}
		r.View.Items = append(r.View.Items, item.View)
		switch item.View.State {
		case "created":
			r.View.CreatedCount++
		case "skipped":
			r.View.SkippedCount++
		case "blocked":
			r.View.BlockedCount++
		}
	}
	a, err := s.readSettlementPlanAuthorization(ctx, q, r.View.AuthorizationID)
	if err != nil {
		return r.View, err
	}
	r.View.State = "pending"
	if len(ids) > 0 {
		r.View.State = "running"
	}
	expected := len(a.Scope.Merchants)
	if r.selection != nil {
		expected = len(r.selection.Snapshot.Selected)
	}
	if len(ids) > expected {
		return r.View, ErrConflict
	}
	if len(ids) == expected {
		r.View.State = "completed"
		if r.View.BlockedCount > 0 {
			r.View.State = "blocked"
			if r.View.CreatedCount > 0 {
				r.View.State = "partial"
			}
		}
	}
	return r.View, nil
}

func (s *Store) SettlementPlanRun(ctx context.Context, id int64, actor SettlementPlanActor) (SettlementPlanRunView, error) {
	if !settlementPlanAdminValid(actor) || id <= 0 {
		return SettlementPlanRunView{}, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return SettlementPlanRunView{}, err
	}
	defer tx.Rollback(ctx)
	v, err := s.settlementPlanRunView(ctx, tx, id)
	if err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}

func (s *Store) SettlementPlanRuns(ctx context.Context, pid, after int64, limit int, actor SettlementPlanActor) ([]SettlementPlanRunView, error) {
	if !settlementPlanAdminValid(actor) || pid <= 0 || after < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = s.readSettlementPlan(ctx, tx, pid); err != nil {
		return nil, err
	}
	ids, err := settlementPlanIDs(ctx, tx, "SELECT id FROM settlement_plan_runs WHERE plan_id=$1 AND id>$2 ORDER BY id LIMIT $3", pid, after, limit)
	if err != nil {
		return nil, err
	}
	out := make([]SettlementPlanRunView, 0, len(ids))
	for _, id := range ids {
		v, e := s.settlementPlanRunView(ctx, tx, id)
		if e != nil {
			return nil, e
		}
		v.Items = nil
		out = append(out, v)
	}
	return out, tx.Commit(ctx)
}
