package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type settlementThresholdRequestKey struct{}
type settlementThresholdRequest struct {
	Tx        pgx.Tx
	Run       settlementPlanRunRecord
	Authority settlementPlanAuthorizationRecord
	Source    settlementThresholdSelection
	Item      SettlementPlanItemView
}

func (s *Store) thresholdRequestPolicy(ctx context.Context, tx pgx.Tx, v *settlementThresholdRequest, mid int64, external string, amount int64, config *int64) (SettlementPolicy, error) {
	if v == nil || tx != v.Tx || v.Item.State != "created" || v.Item.RequestID != 0 || v.Item.MerchantID != mid || external != fmt.Sprintf("SP%d", v.Item.ID) || amount != v.Item.Gross || config == nil || *config != v.Item.RecipientConfigID {
		return SettlementPolicy{}, ErrInvalid
	}
	proof, e := s.thresholdSelectionProof(v.Source, v.Run, v.Authority)
	if e != nil || !hmac.Equal(proof, v.Source.Proof) {
		return SettlementPolicy{}, ErrConflict
	}
	var anchored bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM settlement_plan_runs r JOIN settlement_threshold_selections s ON s.run_id=r.id WHERE r.id=$1 AND r.threshold_source_version=1 AND r.threshold_source_sequence=$2 AND s.source_sequence=$2 AND r.threshold_source_hash=$3 AND s.proof_hash=$3 AND s.transaction_id=pg_current_xact_id())", v.Run.View.ID, v.Source.Sequence, proof).Scan(&anchored); e != nil {
		return SettlementPolicy{}, e
	}
	if !anchored {
		return SettlementPolicy{}, ErrConflict
	}
	for _, p := range v.Source.Snapshot.Planned {
		if p == v.Item {
			for _, m := range v.Authority.Scope.Merchants {
				if m.MerchantID == mid && m.Threshold != nil {
					return SettlementPolicy{FeeRateBPS: m.Threshold.Policy.FeeRateBPS, FeeMaximumCents: m.Threshold.Policy.FeeMaximumCents}, nil
				}
			}
		}
	}
	return SettlementPolicy{}, ErrConflict
}

func (s *Store) thresholdActiveRun(ctx context.Context, tx pgx.Tx, r settlementPlanRunRecord, a settlementPlanAuthorizationRecord) error {
	p, e := s.readSettlementPlan(ctx, tx, r.View.PlanID)
	if e != nil {
		return e
	}
	if !p.View.Enabled || p.Input.Mode != settlementThresholdMode || p.View.Revision != r.View.PlanRevision || p.View.LastAuthorizationID != a.View.ID {
		return ErrConflict
	}
	current, e := s.settlementPlanStaticScope(ctx, tx, p)
	if e != nil {
		return e
	}
	left, _ := json.Marshal(current)
	right, _ := json.Marshal(a.Scope)
	if !bytes.Equal(left, right) {
		return ErrConflict
	}
	actual, e := s.readSettlementPlanAuthorization(ctx, tx, a.View.ID)
	if e != nil {
		return e
	}
	var at time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&at); e != nil {
		return e
	}
	if settlementPlanAuthorityAt(actual.View, at) != "" || at.In(settlementScheduleZone).Format("2006-01-02") != r.View.BusinessDate {
		return ErrState
	}
	return nil
}

func (s *Store) generateThresholdSettlementRun(ctx context.Context, id int64) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var planID int64
	if e = tx.QueryRow(ctx, "SELECT plan_id FROM settlement_plan_runs WHERE id=$1", id).Scan(&planID); e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM settlement_plans WHERE id=$1 FOR UPDATE", planID).Scan(&planID); e != nil {
		return e
	}
	r, e := s.readSettlementPlanRun(ctx, tx, id)
	if e != nil {
		return e
	}
	if r.selection != nil {
		if _, e = s.settlementPlanRunView(ctx, tx, id); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	a, e := s.readSettlementPlanAuthorization(ctx, tx, r.View.AuthorizationID)
	if e != nil {
		return e
	}
	if a.Scope.Threshold == nil {
		return ErrConflict
	}
	source, e := s.sealThresholdSelection(ctx, tx, r)
	if e != nil {
		return e
	}
	for _, proposal := range source.Snapshot.Planned {
		item := settlementPlanItemRecord{RunID: id, View: proposal}
		if proposal.State == "created" {
			consumed := false
			guarded := WithFundsActionGuard(ctx, func(c context.Context, got pgx.Tx, kind, key string) error {
				if got != tx || consumed || kind != "funds-request" || key != fmt.Sprintf("settlements:%d:SP%d", proposal.MerchantID, proposal.ID) {
					return ErrInvalid
				}
				if e := s.thresholdActiveRun(c, got, r, a); e != nil {
					return e
				}
				consumed = true
				return nil
			})
			guarded = context.WithValue(guarded, settlementThresholdRequestKey{}, &settlementThresholdRequest{Tx: tx, Run: r, Authority: a, Source: source, Item: proposal})
			guarded = context.WithValue(guarded, settlementPlanRequestWindowKey{}, settlementPlanRequestWindow{Day: r.View.BusinessDate, Authority: a.View})
			request, e := s.requestFundsTxSettlement(guarded, tx, "settlements", proposal.MerchantID, 0, fmt.Sprintf("SP%d", proposal.ID), proposal.Gross, "system:settlement-scheduler", &SettlementExpectation{FeeAmount: proposal.Fee, NetAmount: proposal.Net}, &proposal.RecipientConfigID)
			if e != nil {
				return e
			}
			if !consumed {
				return ErrConflict
			}
			item.View.RequestID = request.ID
		}
		if e = s.persistSettlementPlanItemRowsTx(ctx, tx, item, r.View, a.View, false); e != nil {
			return e
		}
	}
	if _, e = s.settlementPlanRunView(ctx, tx, id); e != nil {
		return e
	}
	// Audit and request triggers may wait. Check the actual final clock and
	// original scope before making any reserve or authority evidence durable.
	if e = s.thresholdActiveRun(ctx, tx, r, a); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
