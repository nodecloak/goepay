package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (s *Store) checkSettlementAuto(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	var bad int64
	e := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM settlement_auto_heads h LEFT JOIN settlement_auto_authorizations a ON a.id=h.authorization_id WHERE a.id IS NULL OR a.plan_id<>h.plan_id OR EXISTS(SELECT 1 FROM settlement_auto_authorizations n WHERE n.plan_id=h.plan_id AND n.id>a.id))
 +(SELECT count(*) FROM settlement_auto_authorizations a WHERE NOT EXISTS(SELECT 1 FROM settlement_auto_heads h WHERE h.plan_id=a.plan_id))
 +(SELECT count(*) FROM settlement_auto_revocations r WHERE NOT EXISTS(SELECT 1 FROM settlement_auto_authorizations a WHERE a.id=r.authorization_id))
 +(SELECT count(*) FROM settlement_auto_claims c WHERE NOT EXISTS(SELECT 1 FROM settlement_payouts p WHERE p.request_id=c.request_id AND p.transfer_id=c.transfer_id AND p.auto_authorization_id=c.authorization_id AND p.send_state='claimed'))
 +(SELECT count(*) FROM backoffice_audit b WHERE b.resource='settlement-auto-authorizations' AND b.operation IN('authorize','revoke') AND NOT EXISTS(SELECT 1 FROM settlement_auto_authorizations a WHERE a.id=b.resource_id))`).Scan(&bad)
	if e != nil {
		return e
	}
	report.InvalidSettlementPlans += bad
	for after := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, "SELECT id FROM settlement_auto_authorizations WHERE id>$1 ORDER BY id LIMIT 100", after)
		if e != nil {
			return e
		}
		for _, id := range ids {
			if _, e = s.readSettlementAuto(ctx, tx, id); e != nil {
				if !invalidReferralEvidence(e) {
					return e
				}
				report.InvalidSettlementPlans++
			}
			after = id
		}
		if len(ids) < 100 {
			break
		}
	}
	for after := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, `SELECT request_id FROM settlement_payouts p WHERE request_id>$1 AND (auto_authorization_id IS NOT NULL OR EXISTS(SELECT 1 FROM settlement_payout_events e WHERE e.request_id=p.request_id AND e.action='claim' AND e.actor_role='system')) ORDER BY request_id LIMIT 100`, after)
		if e != nil {
			return e
		}
		for _, id := range ids {
			if e = s.verifySettlementAutoPayout(ctx, tx, id); e != nil {
				if !invalidReferralEvidence(e) {
					return e
				}
				report.InvalidSettlementPlans++
			}
			after = id
		}
		if len(ids) < 100 {
			return nil
		}
	}
}
