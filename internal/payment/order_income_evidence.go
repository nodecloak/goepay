package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Evidence is original income less independently proved uses of that order.
// Current wallet funds cannot enlarge this source budget.
type orderIncomeSource struct {
	MerchantNet, RefundMerchantAmount, ChannelSharingAmount, InternalAllocatedAmount, Remaining int64
}

func (s *Store) orderIncomeSource(ctx context.Context, q pgx.Tx, o Order) (orderIncomeSource, error) {
	var v orderIncomeSource
	if o.Status != "paid" || o.MerchantAmount <= 0 {
		return v, ErrState
	}
	if e := s.verifyOrderSharingReturnEvidence(ctx, q, o.ID); e != nil {
		return v, e
	}
	if e := s.verifyOrderSharingFinishEvidence(ctx, q, o.ID); e != nil {
		return v, e
	}
	if e := s.verifyOrderRefundFundingHistory(ctx, q, o.ID); e != nil {
		return v, e
	}
	var owner, pending int64
	var valid bool
	e := q.QueryRow(ctx, "SELECT merchant_id,merchant_net,refund_merchant_amount,channel_sharing_amount,internal_allocated_amount,remaining,pending_refunds,valid FROM order_income_source_evidence WHERE order_id=$1", o.ID).Scan(&owner, &v.MerchantNet, &v.RefundMerchantAmount, &v.ChannelSharingAmount, &v.InternalAllocatedAmount, &v.Remaining, &pending, &valid)
	if e != nil {
		return v, e
	}
	if !valid || owner != o.MerchantID || v.MerchantNet != o.MerchantAmount || pending != 0 || v.Remaining <= 0 {
		return orderIncomeSource{}, ErrState
	}
	return v, nil
}
func (s *Store) orderIncomeRemaining(ctx context.Context, q pgx.Tx, o Order) (int64, error) {
	v, e := s.orderIncomeSource(ctx, q, o)
	return v.Remaining, e
}

func (s *Store) OrderIncomeFreezable(ctx context.Context, merchantID int64, tradeNo string) (int64, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE merchant_id=$1 AND trade_no=$2", merchantID, tradeNo))
	if e != nil {
		return 0, e
	}
	if e = s.orderIncomeRefundAllowed(ctx, tx, o.ID); e != nil {
		return 0, e
	}
	amount, e := s.orderIncomeRemaining(ctx, tx, o)
	if e != nil {
		return 0, e
	}
	var balance int64
	if e = tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=$1 AND kind='available'", merchantID).Scan(&balance); e != nil {
		return 0, e
	}
	if balance < amount {
		return 0, ErrInsufficient
	}
	return amount, tx.Commit(ctx)
}
