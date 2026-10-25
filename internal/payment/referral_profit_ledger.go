package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (s *Store) checkReferralProfitOrphans(ctx context.Context, tx pgx.Tx, r *LedgerCheck) error {
	var count int64
	err := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM referral_profit_costs c WHERE NOT EXISTS(SELECT 1 FROM referral_order_terms t WHERE t.order_id=c.order_id))
 +(SELECT count(*) FROM referral_profit_cost_events e WHERE NOT EXISTS(SELECT 1 FROM referral_profit_costs c WHERE c.order_id=e.order_id))`).Scan(&count)
	if err != nil {
		return err
	}
	r.InvalidReferralTerms += count
	return nil
}
