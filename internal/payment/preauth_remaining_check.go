package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Store) checkAuthorizationReleases(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	// Start with all independent anchors. Missing parents do not disappear in
	// an inner join, including the original read-only recovery job.
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, `SELECT id FROM (
   SELECT order_id id FROM payment_authorization_releases UNION SELECT order_id FROM authorization_remaining_sources
   UNION SELECT order_id FROM authorization_remaining_claims
   UNION SELECT o.id FROM backoffice_audit b JOIN orders o ON o.id::text=b.changes->>'order_id' WHERE b.resource='authorization-remaining-sources'
   UNION SELECT o.id FROM jobs j JOIN orders o ON o.trade_no=j.payload->>'trade_no' WHERE j.kind='authorization.release.reconcile'
  ) evidence WHERE id>$1 ORDER BY id LIMIT 100`, cursor)
		if e != nil {
			return e
		}
		for _, id := range ids {
			if _, e = s.readAuthorizationReleaseTx(ctx, tx, id); e != nil {
				if errors.Is(e, ErrConflict) || errors.Is(e, ErrNotFound) || errors.Is(e, ErrPreauthObservation) {
					report.InvalidAuthorizationReleases++
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
	var orphan int64
	if e := tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit b WHERE b.resource='authorization-remaining-sources' AND NOT EXISTS(SELECT 1 FROM orders o WHERE o.id::text=b.changes->>'order_id')`).Scan(&orphan); e != nil {
		return e
	}
	report.InvalidAuthorizationReleases += orphan
	return nil
}
