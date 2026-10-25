package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type settlementThresholdCandidate struct {
	MerchantID      int64 `json:"merchant_id,string"`
	LedgerAvailable int64 `json:"ledger_available,string"`
	Eligible        int64 `json:"eligible,string"`
}
type settlementThresholdSnapshot struct {
	Candidates       []settlementThresholdCandidate `json:"candidates"`
	Selected         []string                       `json:"selected"`
	PreviousRunID    int64                          `json:"previous_run_id,string"`
	PreviousSequence int64                          `json:"previous_sequence,string"`
	PreviousProof    []byte                         `json:"previous_proof"`
	BeforeGross      int64                          `json:"before_gross,string"`
	BeforeItems      int64                          `json:"before_items,string"`
	AfterGross       int64                          `json:"after_gross,string"`
	AfterItems       int64                          `json:"after_items,string"`
	Planned          []SettlementPlanItemView       `json:"planned"`
}
type settlementThresholdSelection struct {
	RunID         int64
	Sequence      int64
	Snapshot      settlementThresholdSnapshot
	CreatedAt     time.Time
	TransactionID string
	Proof         []byte
}

func thresholdSelectedWallets(candidates []settlementThresholdCandidate, threshold int64) []string {
	selected := []string{}
	for i := len(candidates) - 1; i >= 0 && len(selected) < 5; i-- {
		if candidates[i].LedgerAvailable > threshold {
			selected = append(selected, strconv.FormatInt(candidates[i].MerchantID, 10))
		}
	}
	return selected
}

func (s *Store) thresholdSelectionProof(v settlementThresholdSelection, r settlementPlanRunRecord, a settlementPlanAuthorizationRecord) ([]byte, error) {
	return s.referralDigest("settlement-threshold74/selection", []any{v.RunID, v.Sequence, r.Proof, a.Proof, v.Snapshot, v.CreatedAt.UTC().Format(time.RFC3339Nano), v.TransactionID})
}

// The original run has already been authenticated by readSettlementPlanRun.
// This helper authenticates its independent choice without calling readRun
// recursively, so the choice can later become part of every normal run read.
func (s *Store) readThresholdSelection(ctx context.Context, q fundsReader, r settlementPlanRunRecord) (settlementThresholdSelection, error) {
	var out settlementThresholdSelection
	if q == nil || r.View.ID <= 0 {
		return out, ErrInvalid
	}
	base, e := s.settlementPlanRunProof(r)
	if e != nil || !hmac.Equal(base, r.Proof) {
		return out, ErrConflict
	}
	a, e := s.readSettlementPlanAuthorization(ctx, q, r.View.AuthorizationID)
	if e != nil {
		return out, e
	}
	if a.Scope.Threshold == nil || a.View.PlanID != r.View.PlanID || a.View.PlanRevision != r.View.PlanRevision {
		return out, ErrConflict
	}
	var version int
	var parent, runProof []byte
	var parentSequence int64
	var items int
	var audited bool
	e = q.QueryRow(ctx, `SELECT r.threshold_source_version,r.threshold_source_hash,r.proof_hash,r.threshold_source_sequence,(SELECT count(*) FROM settlement_plan_items WHERE run_id=r.id),EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-threshold-selections' AND b.resource_id=r.id AND b.operation='select' AND (b.actor_role,b.actor_id,b.actor_name)=('system',0::bigint,'settlement-scheduler')) FROM settlement_plan_runs r WHERE r.id=$1`, r.View.ID).Scan(&version, &parent, &runProof, &parentSequence, &items, &audited)
	if e != nil {
		return out, e
	}
	if version != 1 || len(parent) != 32 || !hmac.Equal(runProof, r.Proof) || !audited {
		return out, ErrConflict
	}
	var raw []byte
	e = q.QueryRow(ctx, "SELECT run_id,source_sequence,snapshot,created_at,transaction_id::text,proof_hash FROM settlement_threshold_selections WHERE run_id=$1", r.View.ID).Scan(&out.RunID, &out.Sequence, &raw, &out.CreatedAt, &out.TransactionID, &out.Proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ErrConflict
	}
	if e != nil {
		return out, e
	}
	if json.Unmarshal(raw, &out.Snapshot) != nil || out.Sequence <= 0 || out.Sequence != parentSequence || out.RunID != r.View.ID || out.Snapshot.Selected == nil || len(out.Snapshot.Candidates) != len(a.Scope.Merchants) || items != len(out.Snapshot.Selected) || out.CreatedAt.Before(r.View.CreatedAt) || out.CreatedAt.Before(a.View.ValidFrom) || !out.CreatedAt.Before(a.View.ExpiresAt) || out.CreatedAt.In(settlementScheduleZone).Format("2006-01-02") != r.View.BusinessDate {
		return out, ErrConflict
	}
	for i, c := range out.Snapshot.Candidates {
		if c.MerchantID != a.Scope.Merchants[i].MerchantID || c.LedgerAvailable < 0 || c.Eligible < 0 || c.Eligible > c.LedgerAvailable {
			return out, ErrConflict
		}
	}
	if !slices.Equal(out.Snapshot.Selected, thresholdSelectedWallets(out.Snapshot.Candidates, a.Scope.Threshold.Policy.ThresholdCents)) {
		return out, ErrConflict
	}
	proof, e := s.thresholdSelectionProof(out, r, a)
	if e != nil || !hmac.Equal(proof, out.Proof) || !hmac.Equal(parent, out.Proof) {
		return out, ErrConflict
	}
	var revoked bool
	if e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM settlement_plan_revocations WHERE authorization_id=$1 AND created_at<=$2)", a.View.ID, out.CreatedAt).Scan(&revoked); e != nil {
		return out, e
	}
	if revoked {
		return out, ErrConflict
	}
	ids := make([]int64, len(out.Snapshot.Selected))
	for i, mid := range out.Snapshot.Selected {
		ids[i], e = strconv.ParseInt(mid, 10, 64)
		if e != nil {
			return out, ErrConflict
		}
	}
	var complete bool
	if e = q.QueryRow(ctx, "SELECT count(*)=cardinality($2::bigint[]) AND count(DISTINCT merchant_id)=cardinality($2::bigint[]) AND COALESCE(bool_and(merchant_id=ANY($2::bigint[])),true) FROM settlement_plan_items WHERE run_id=$1", r.View.ID, ids).Scan(&complete); e != nil {
		return out, e
	}
	if !complete {
		return out, ErrConflict
	}
	if e = s.verifyThresholdPlannedItems(ctx, q, r, a, out); e != nil {
		return out, e
	}
	return out, nil
}

// This primitive captures real locked wallet facts; it accepts no caller amount
// or selected IDs. The owner must persist every member's outcome and commit
// the whole transaction. The deferred SQL constraint rejects partial choices.
func (s *Store) sealThresholdSelection(ctx context.Context, tx pgx.Tx, r settlementPlanRunRecord) (settlementThresholdSelection, error) {
	out := settlementThresholdSelection{RunID: r.View.ID}
	if tx == nil || r.View.ID <= 0 {
		return out, ErrInvalid
	}
	base, e := s.settlementPlanRunProof(r)
	if e != nil || !hmac.Equal(base, r.Proof) {
		return out, ErrConflict
	}
	var plan int64
	if e := tx.QueryRow(ctx, "SELECT id FROM settlement_plans WHERE id=$1 FOR UPDATE", r.View.PlanID).Scan(&plan); e != nil {
		return out, e
	}
	p, e := s.readSettlementPlan(ctx, tx, plan)
	if e != nil {
		return out, e
	}
	if !p.View.Enabled || p.Input.Mode != settlementThresholdMode || p.View.Revision != r.View.PlanRevision || p.View.LastAuthorizationID != r.View.AuthorizationID {
		return out, ErrConflict
	}
	a, e := s.readSettlementPlanAuthorization(ctx, tx, r.View.AuthorizationID)
	if e != nil {
		return out, e
	}
	if a.Scope.Threshold == nil {
		return out, ErrConflict
	}
	var version int
	var anchor, runProof []byte
	var previousAudit bool
	if e = tx.QueryRow(ctx, `SELECT threshold_source_version,threshold_source_hash,proof_hash,EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-threshold-selections' AND b.resource_id=r.id AND b.operation='select') FROM settlement_plan_runs r WHERE id=$1`, r.View.ID).Scan(&version, &anchor, &runProof, &previousAudit); e != nil {
		return out, e
	}
	if version != 0 || anchor != nil || !hmac.Equal(runProof, r.Proof) || previousAudit {
		return out, ErrConflict
	}
	rows, e := tx.Query(ctx, "SELECT id FROM merchants WHERE id=ANY($1::bigint[]) ORDER BY id FOR NO KEY UPDATE", p.Input.MerchantIDs)
	if e != nil {
		return out, e
	}
	for rows.Next() {
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if _, e = readSettlementThresholdConfig(ctx, tx, true); e != nil {
		return out, e
	}
	groupIDs := []int64{}
	for _, m := range a.Scope.Merchants {
		if m.Threshold == nil {
			return out, ErrConflict
		}
		if m.Threshold.GroupID > 0 {
			groupIDs = append(groupIDs, m.Threshold.GroupID)
		}
	}
	slices.Sort(groupIDs)
	groupIDs = slices.Compact(groupIDs)
	if len(groupIDs) > 0 {
		rows, e = tx.Query(ctx, "SELECT id FROM backoffice_records WHERE resource='groups' AND id=ANY($1::bigint[]) ORDER BY id FOR SHARE", groupIDs)
		if e != nil {
			return out, e
		}
		for rows.Next() {
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
	}
	for _, mid := range p.Input.MerchantIDs {
		if e = requireOutgoingFunds(ctx, tx, mid, 0); e != nil {
			return out, e
		}
	}
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp(),pg_current_xact_id()::text").Scan(&out.CreatedAt, &out.TransactionID); e != nil {
		return out, e
	}
	if settlementPlanAuthorityAt(a.View, out.CreatedAt) != "" || out.CreatedAt.In(settlementScheduleZone).Format("2006-01-02") != r.View.BusinessDate {
		return out, ErrState
	}
	current, e := s.settlementPlanStaticScope(ctx, tx, p)
	if e != nil {
		return out, e
	}
	want, _ := json.Marshal(a.Scope)
	actual, _ := json.Marshal(current)
	if !bytes.Equal(want, actual) {
		return out, ErrConflict
	}
	out.Snapshot.Candidates = []settlementThresholdCandidate{}
	for _, mid := range p.Input.MerchantIDs {
		funds, e := readFundsAvailability(ctx, tx, mid, &out.CreatedAt)
		if e != nil {
			return out, e
		}
		out.Snapshot.Candidates = append(out.Snapshot.Candidates, settlementThresholdCandidate{MerchantID: mid, LedgerAvailable: funds.LedgerAvailable, Eligible: funds.Available})
	}
	out.Snapshot.Selected = thresholdSelectedWallets(out.Snapshot.Candidates, a.Scope.Threshold.Policy.ThresholdCents)
	if e = s.thresholdPreviousConsumption(ctx, tx, r, &out); e != nil {
		return out, e
	}
	ids := make([]int64, len(out.Snapshot.Selected))
	for i := range ids {
		if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('settlement_plan_items','id'))").Scan(&ids[i]); e != nil {
			return out, e
		}
	}
	out.Snapshot.Planned, out.Snapshot.AfterGross, out.Snapshot.AfterItems, e = planThresholdItems(a, out.Snapshot.Candidates, ids, out.Snapshot.BeforeGross, out.Snapshot.BeforeItems, out.CreatedAt)
	if e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('settlement_threshold_selections','source_sequence'))").Scan(&out.Sequence); e != nil {
		return out, e
	}
	out.Proof, e = s.thresholdSelectionProof(out, r, a)
	if e != nil {
		return out, e
	}
	raw, _ := json.Marshal(out.Snapshot)
	if _, e = tx.Exec(ctx, "INSERT INTO settlement_threshold_selections(run_id,snapshot,created_at,transaction_id,proof_hash,source_sequence)VALUES($1,$2,$3,$4::xid8,$5,$6)", r.View.ID, raw, out.CreatedAt, out.TransactionID, out.Proof, out.Sequence); e != nil {
		return out, e
	}
	if e = settlementPlanAudit(ctx, tx, "system", 0, "settlement-scheduler", "settlement-threshold-selections", r.View.ID, "select", map[string]string{"run_id": fmt.Sprint(r.View.ID), "selected_count": fmt.Sprint(len(out.Snapshot.Selected))}); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "UPDATE settlement_plan_runs SET threshold_source_version=1,threshold_source_hash=$2,threshold_source_sequence=$3 WHERE id=$1", r.View.ID, out.Proof, out.Sequence); e != nil {
		return out, e
	}
	return out, nil
}

func (s *Store) verifyThresholdRunSource(ctx context.Context, q fundsReader, r settlementPlanRunRecord, mode string) (*settlementThresholdSelection, error) {
	var version, items int
	var sequence int64
	var hash []byte
	var child, audit bool
	e := q.QueryRow(ctx, `SELECT threshold_source_version,threshold_source_hash,threshold_source_sequence,(SELECT count(*) FROM settlement_plan_items WHERE run_id=r.id),EXISTS(SELECT 1 FROM settlement_threshold_selections WHERE run_id=r.id),EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-threshold-selections' AND b.resource_id=r.id AND b.operation='select') FROM settlement_plan_runs r WHERE id=$1`, r.View.ID).Scan(&version, &hash, &sequence, &items, &child, &audit)
	if e != nil {
		return nil, e
	}
	if mode != settlementThresholdMode {
		if version != 0 || hash != nil || sequence != 0 || child || audit {
			return nil, ErrConflict
		}
		return nil, nil
	}
	if version == 0 {
		if hash != nil || sequence != 0 || child || audit || items != 0 {
			return nil, ErrConflict
		}
		return nil, nil
	}
	if version != 1 || len(hash) != 32 || sequence <= 0 || !child || !audit {
		return nil, ErrConflict
	}
	v, e := s.readThresholdSelection(ctx, q, r)
	if e != nil {
		return nil, e
	}
	return &v, nil
}
