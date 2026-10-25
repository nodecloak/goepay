package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (s *Store) checkThresholdSelections(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	var bad int64
	e := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM settlement_threshold_selections s WHERE NOT EXISTS(SELECT 1 FROM settlement_plan_runs r JOIN settlement_plan_versions v ON (v.plan_id,v.revision)=(r.plan_id,r.plan_revision) WHERE r.id=s.run_id AND v.mode='balance_threshold' AND r.threshold_source_version=1 AND r.threshold_source_hash=s.proof_hash AND r.threshold_source_sequence=s.source_sequence))
 +(SELECT count(*) FROM backoffice_audit b WHERE b.resource='settlement-threshold-selections' AND b.operation='select' AND ((b.actor_role,b.actor_id,b.actor_name) IS DISTINCT FROM ('system',0::bigint,'settlement-scheduler') OR NOT EXISTS(SELECT 1 FROM settlement_threshold_selections s WHERE s.run_id=b.resource_id)))
 +(SELECT count(*) FROM settlement_threshold_intents x WHERE NOT EXISTS(SELECT 1 FROM settlement_plan_items i JOIN settlement_plan_runs r ON r.id=i.run_id JOIN settlement_plan_versions v ON (v.plan_id,v.revision)=(r.plan_id,r.plan_revision) WHERE v.mode='balance_threshold' AND i.state='created' AND (i.id,i.run_id,i.merchant_id,i.request_id)=(x.item_id,x.run_id,x.merchant_id,x.request_id)))`).Scan(&bad)
	if e == nil {
		report.InvalidSettlementPlans += bad
	}
	return e
}
