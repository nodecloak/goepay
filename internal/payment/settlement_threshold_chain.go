package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// The plan lock serializes sources. Structural checks include every original
// parent, so losing an older child cannot refill a finite plan/day budget.
func (s *Store) thresholdPreviousConsumption(ctx context.Context, tx pgx.Tx, r settlementPlanRunRecord, out *settlementThresholdSelection) error {
	var bad bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlement_plan_runs r LEFT JOIN settlement_threshold_selections s ON s.run_id=r.id WHERE r.plan_id=$1 AND r.business_date=$2::date AND (
 (r.threshold_source_version=1 AND (s.run_id IS NULL OR s.proof_hash IS DISTINCT FROM r.threshold_source_hash OR s.source_sequence<>r.threshold_source_sequence))
 OR (r.threshold_source_version=0 AND (s.run_id IS NOT NULL OR EXISTS(SELECT 1 FROM settlement_plan_items i WHERE i.run_id=r.id) OR EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-threshold-selections' AND b.resource_id=r.id AND b.operation='select')))))`, r.View.PlanID, r.View.BusinessDate).Scan(&bad)
	if e != nil {
		return e
	}
	if bad {
		return ErrConflict
	}
	var id int64
	var gross, count int64
	if e = tx.QueryRow(ctx, "SELECT COALESCE(sum(gross),0)::bigint,count(*) FROM settlement_plan_budget_uses WHERE plan_id=$1 AND business_date=$2::date", r.View.PlanID, r.View.BusinessDate).Scan(&gross, &count); e != nil {
		return e
	}
	e = tx.QueryRow(ctx, "SELECT id FROM settlement_plan_runs WHERE plan_id=$1 AND business_date=$2::date AND threshold_source_version=1 ORDER BY threshold_source_sequence DESC LIMIT 1", r.View.PlanID, r.View.BusinessDate).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		if gross != 0 || count != 0 {
			return ErrConflict
		}
		return nil
	}
	if e != nil {
		return e
	}
	previous, e := s.readSettlementPlanRun(ctx, tx, id)
	if e != nil {
		return e
	}
	if previous.selection == nil {
		return ErrConflict
	}
	p := previous.selection
	if gross != p.Snapshot.AfterGross || count != p.Snapshot.AfterItems {
		return ErrConflict
	}
	out.Snapshot.PreviousRunID, out.Snapshot.PreviousSequence, out.Snapshot.PreviousProof = p.RunID, p.Sequence, p.Proof
	out.Snapshot.BeforeGross, out.Snapshot.BeforeItems = p.Snapshot.AfterGross, p.Snapshot.AfterItems
	return nil
}

// Verify the signed cumulative summary and its original item proposal without
// readItem -> readRun recursion. Subsequent ordinary item reads retain all
// original ledger/request invariants.
func (s *Store) verifyThresholdPlannedItems(ctx context.Context, q fundsReader, r settlementPlanRunRecord, a settlementPlanAuthorizationRecord, out settlementThresholdSelection) error {
	v := out.Snapshot
	if v.Planned == nil || len(v.Planned) != len(v.Selected) {
		return ErrConflict
	}
	var priorID, priorSequence int64
	var priorProof []byte
	e := q.QueryRow(ctx, "SELECT id,threshold_source_sequence,threshold_source_hash FROM settlement_plan_runs WHERE plan_id=$1 AND business_date=$2::date AND threshold_source_version=1 AND threshold_source_sequence<$3 ORDER BY threshold_source_sequence DESC LIMIT 1", r.View.PlanID, r.View.BusinessDate, out.Sequence).Scan(&priorID, &priorSequence, &priorProof)
	if errors.Is(e, pgx.ErrNoRows) {
		if v.PreviousRunID != 0 || v.PreviousSequence != 0 || len(v.PreviousProof) != 0 || v.BeforeGross != 0 || v.BeforeItems != 0 {
			return ErrConflict
		}
	} else {
		if e != nil {
			return e
		}
		if v.PreviousRunID != priorID || v.PreviousSequence != priorSequence || !hmac.Equal(v.PreviousProof, priorProof) {
			return ErrConflict
		}
	}
	ids := make([]int64, len(v.Planned))
	for i, p := range v.Planned {
		ids[i] = p.ID
	}
	// Revocations have already been authenticated and checked against source
	// time. A later revocation cannot invalidate an original historical quote.
	a.View.Revoked = false
	planned, gross, count, e := planThresholdItems(a, v.Candidates, ids, v.BeforeGross, v.BeforeItems, out.CreatedAt)
	if e != nil || gross != v.AfterGross || count != v.AfterItems {
		return ErrConflict
	}
	left, _ := json.Marshal(planned)
	right, _ := json.Marshal(v.Planned)
	if !bytes.Equal(left, right) {
		return ErrConflict
	}
	for _, p := range planned {
		var item settlementPlanItemRecord
		var audited bool
		e = q.QueryRow(ctx, `SELECT i.id,i.run_id,i.merchant_id,COALESCE(i.request_id,0),i.state,i.reason,i.ledger_available,i.eligible,i.retained,i.gross,i.fee,i.net,COALESCE(i.recipient_config_id,0),i.created_at,i.proof_hash,EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-plan-items' AND b.resource_id=i.id AND b.operation='generate' AND (b.actor_role,b.actor_id,b.actor_name)=('system',0::bigint,'settlement-scheduler')) FROM settlement_plan_items i WHERE i.id=$1`, p.ID).Scan(&item.View.ID, &item.RunID, &item.View.MerchantID, &item.View.RequestID, &item.View.State, &item.View.Reason, &item.View.LedgerAvailable, &item.View.Eligible, &item.View.Retained, &item.View.Gross, &item.View.Fee, &item.View.Net, &item.View.RecipientConfigID, &item.CreatedAt, &item.Proof, &audited)
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrConflict
		}
		if e != nil {
			return e
		}
		proof, e := s.settlementPlanItemProof(item)
		if e != nil || !hmac.Equal(proof, item.Proof) || !audited || item.RunID != r.View.ID || (p.State == "created") != (item.View.RequestID > 0) {
			return ErrConflict
		}
		actual := item.View
		actual.RequestID = 0
		if actual != p {
			return ErrConflict
		}
	}
	return nil
}
