package payment

import (
	"context"

	"goepay/internal/ledger"
	"goepay/internal/merchantpolicy"
)

// Existing immutable splits do not depend on the current merchant group. New
// splits take the same merchant/group -> order lock order as payment admission.
// ProviderFlow has already returned any original provider attempt without
// granting another send opportunity before it reaches this preparation.
func (s *Store) prepareCombineSubOrders207(ctx context.Context, order Order, amount int64) ([]CombineSubOrder207, error) {
	existing, err := s.CombineSubOrders(ctx, order.ID)
	if err != nil || len(existing) > 0 {
		return existing, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	policy, err := merchantpolicy.ReadCombinePolicy(ctx, tx, order.MerchantID, true)
	if err != nil {
		return nil, err
	}
	stored, err := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1 FOR UPDATE", order.ID))
	if err != nil {
		return nil, err
	}
	if stored.MerchantID != order.MerchantID || stored.TradeNo != order.TradeNo {
		return nil, ErrConflict
	}
	existing, err = s.combineSubOrdersTx(ctx, tx, order.ID, false)
	if err != nil || len(existing) > 0 {
		return existing, err
	}
	charge, _, err := s.authenticatedOrderCharge104(ctx, tx, stored)
	if err != nil {
		return nil, err
	}
	if charge != amount {
		return nil, ErrConflict
	}
	if err = policy.Recheck(ctx, tx, order.MerchantID); err != nil {
		return nil, err
	}
	open := "0"
	if policy.Enabled {
		open = "1"
	}
	amounts, split := SplitCombine207(Combine207{Open: open, MinMoney: ledger.Format(policy.MinCents), SubMoney: ledger.Format(policy.SubCents)}, amount)
	if split {
		existing, err = s.CreateCombineSubOrdersTx(ctx, tx, order.ID, order.TradeNo, amounts)
		if err != nil {
			return nil, err
		}
	}
	if err = policy.Recheck(ctx, tx, order.MerchantID); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return existing, nil
}
