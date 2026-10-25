package payment

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Count from every parent, then authenticate bounded pages. A missing joined
// projection must never erase malformed money or authority evidence.
func (s *Store) checkSettlementPlans(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	types := []struct {
		table, view string
		read        func(context.Context, referralReader, int64) error
	}{
		{"settlement_plans", "settlement_plan_head_invariants", func(c context.Context, q referralReader, id int64) error {
			_, e := s.readSettlementPlan(c, q, id)
			return e
		}},
		{"settlement_plan_versions", "settlement_plan_definition_invariants", func(c context.Context, q referralReader, id int64) error {
			_, e := s.readSettlementPlanDefinition(c, q, id)
			return e
		}},
		{"settlement_schedule_preferences", "settlement_plan_preference_invariants", func(c context.Context, q referralReader, id int64) error {
			_, e := s.readSettlementPreferenceVersion(c, q, id)
			return e
		}},
		{"settlement_plan_authorizations", "settlement_plan_authorization_invariants", func(c context.Context, q referralReader, id int64) error {
			_, e := s.readSettlementPlanAuthorization(c, q, id)
			return e
		}},
		{"settlement_plan_runs", "settlement_plan_run_invariants", func(c context.Context, q referralReader, id int64) error {
			_, e := s.readSettlementPlanRun(c, q, id)
			return e
		}},
		{"settlement_plan_items", "settlement_plan_item_invariants", func(c context.Context, q referralReader, id int64) error {
			_, e := s.readSettlementPlanItem(c, q, id)
			return e
		}},
	}
	for _, kind := range types {
		var bad int64
		// Names come from this closed source allowlist, never an HTTP field.
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+kind.table+" t WHERE NOT EXISTS(SELECT 1 FROM "+kind.view+" v WHERE v.id=t.id AND v.valid IS TRUE)").Scan(&bad); err != nil {
			return err
		}
		report.InvalidSettlementPlans += bad
		for after := int64(0); ; {
			ids, err := sharingEvidenceIDsPage(ctx, tx, "SELECT t.id FROM "+kind.table+" t WHERE t.id>$1 AND EXISTS(SELECT 1 FROM "+kind.view+" v WHERE v.id=t.id AND v.valid IS TRUE) ORDER BY t.id LIMIT 100", after)
			if err != nil {
				return err
			}
			for _, id := range ids {
				if err = kind.read(ctx, tx, id); err != nil {
					if invalidReferralEvidence(err) {
						report.InvalidSettlementPlans++
					} else {
						return err
					}
				}
				after = id
			}
			if len(ids) < 100 {
				break
			}
		}
	}
	var orphaned int64
	err := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM settlement_schedule_preference_heads h WHERE NOT EXISTS(SELECT 1 FROM settlement_schedule_preferences p WHERE (p.id,p.merchant_id,p.revision)=(h.preference_id,h.merchant_id,h.revision)))
 +(SELECT count(*) FROM settlement_plan_budget_uses b WHERE NOT EXISTS(SELECT 1 FROM settlement_plan_items i JOIN settlement_plan_runs r ON r.id=i.run_id WHERE i.id=b.item_id AND i.state='created' AND (r.authorization_id,r.plan_id,r.business_date,i.gross,i.fee,i.net)=(b.authorization_id,b.plan_id,b.business_date,b.gross,b.fee,b.net)))
 +(SELECT count(*) FROM settlement_scheduled_intents x WHERE NOT EXISTS(SELECT 1 FROM settlement_plan_items i JOIN settlement_plan_runs r ON r.id=i.run_id WHERE i.id=x.item_id AND i.state='created' AND (i.merchant_id,r.business_date,i.request_id)=(x.merchant_id,x.business_date,x.request_id)))
 +(SELECT count(*) FROM ledger_requests r WHERE (r.settlement_plan_item_id IS NOT NULL OR EXISTS(SELECT 1 FROM ledger_request_events e WHERE e.request_id=r.id AND e.actor='system:settlement-scheduler')) AND NOT EXISTS(SELECT 1 FROM settlement_plan_items i WHERE i.id=r.settlement_plan_item_id AND i.state='created' AND i.request_id=r.id))`).Scan(&orphaned)
	if err != nil {
		return err
	}
	report.InvalidSettlementPlans += orphaned
	if err = s.checkThresholdSelections(ctx, tx, report); err != nil {
		return err
	}
	return s.checkSettlementAuto(ctx, tx, report)
}
