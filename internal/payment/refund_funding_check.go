package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (s *Store) checkIncomeRefundFundings(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	if e := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM order_income_holds h WHERE NOT EXISTS(SELECT 1 FROM order_income_hold_funding_evidence v WHERE v.id=h.id AND v.valid IS TRUE))
 +(SELECT count(*) FROM order_income_hold_fundings f WHERE NOT EXISTS(SELECT 1 FROM order_income_holds h WHERE h.id=f.hold_id))
 +(SELECT count(*) FROM accounts a WHERE a.kind='order_income_reserve' AND NOT EXISTS(SELECT 1 FROM order_income_hold_fundings f WHERE f.account_id=a.id))
 +(SELECT count(*) FROM order_income_hold_releases z WHERE NOT EXISTS(SELECT 1 FROM order_income_hold_fundings f WHERE f.hold_id=z.hold_id)),
 (SELECT count(*) FROM refunds r WHERE r.funding_evidence_version=2 AND NOT EXISTS(SELECT 1 FROM refund_funding_invariants v WHERE v.refund_id=r.id AND v.valid IS TRUE))
 +(SELECT count(*) FROM refund_fundings f WHERE NOT EXISTS(SELECT 1 FROM refunds r WHERE r.id=f.refund_id))
 +(SELECT count(*) FROM accounts a WHERE a.kind='order_income_refund_reserve' AND NOT EXISTS(SELECT 1 FROM refund_fundings f WHERE f.reserve_account_id=a.id))
 +(SELECT count(*) FROM refund_funding_resolutions z WHERE NOT EXISTS(SELECT 1 FROM refund_fundings f WHERE f.refund_id=z.refund_id))`).Scan(&report.InvalidOrderIncomeFundings, &report.InvalidRefundFundings); e != nil {
		return e
	}
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, `SELECT f.hold_id FROM order_income_hold_fundings f WHERE f.hold_id>$1 AND EXISTS(SELECT 1 FROM order_income_hold_funding_evidence v WHERE v.id=f.hold_id AND v.valid IS TRUE) ORDER BY f.hold_id LIMIT 100`, cursor)
		if e != nil {
			return e
		}
		for _, id := range ids {
			h, e := scanIncomeHold(tx.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE h.id=$1", id))
			if e == nil {
				_, _, _, e = s.readIncomeHold(ctx, tx, h)
			}
			if e != nil {
				if invalidSharingEvidence(e) {
					report.InvalidOrderIncomeFundings++
				} else {
					return e
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, `SELECT f.refund_id FROM refund_fundings f WHERE f.refund_id>$1 AND EXISTS(SELECT 1 FROM refund_funding_invariants v WHERE v.refund_id=f.refund_id AND v.valid IS TRUE) ORDER BY f.refund_id LIMIT 100`, cursor)
		if e != nil {
			return e
		}
		for _, id := range ids {
			r, e := scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", id))
			if e == nil {
				e = s.verifyProviderRefundResult(ctx, tx, r)
			}
			if e == nil {
				e = s.verifyRefundFundingEvidence(ctx, tx, r)
			}
			if e != nil {
				if invalidSharingEvidence(e) {
					report.InvalidRefundFundings++
				} else {
					return e
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	return nil
}
