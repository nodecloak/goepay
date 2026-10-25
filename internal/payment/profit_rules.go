package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strconv"
)

// ExecuteProfitRule calculates a fixed-point share of a paid order, reserves the
// request, and records allocation in one transaction. Explicit review is separate.
func (s *Store) ExecuteProfitRule(ctx context.Context, ruleID int64, tradeNo, actor string) (LedgerRequest, error) {
	if ruleID <= 0 || tradeNo == "" || actor == "" {
		return LedgerRequest{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return LedgerRequest{}, e
	}
	defer tx.Rollback(ctx)
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE trade_no=$1 FOR UPDATE", tradeNo))
	if e != nil {
		return LedgerRequest{}, e
	}
	var existing int64
	e = tx.QueryRow(ctx, "SELECT request_id FROM order_profit_allocations WHERE order_id=$1 AND rule_id=$2", o.ID, ruleID).Scan(&existing)
	if e == nil {
		return scanLedgerRequest(tx.QueryRow(ctx, "SELECT "+requestColumns+" FROM ledger_requests WHERE id=$1", existing))
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return LedgerRequest{}, e
	}
	if o.Status != "paid" {
		return LedgerRequest{}, ErrState
	}
	if e = s.orderIncomeRefundAllowed(ctx, tx, o.ID); e != nil {
		return LedgerRequest{}, e
	}
	var owner int64
	var rateRaw, recipientRaw string
	e = tx.QueryRow(ctx, "SELECT owner_id,body->>'rate_bps',body->>'recipient_id' FROM backoffice_records WHERE resource='profitsharing-rules' AND id=$1 AND body->>'enabled'='true' FOR SHARE", ruleID).Scan(&owner, &rateRaw, &recipientRaw)
	if e != nil {
		return LedgerRequest{}, ErrNotFound
	}
	if owner != o.MerchantID {
		return LedgerRequest{}, ErrConflict
	}
	rate, e := strconv.ParseInt(rateRaw, 10, 64)
	if e != nil || rate < 1 || rate > 10000 {
		return LedgerRequest{}, ErrInvalid
	}
	recipient, e := strconv.ParseInt(recipientRaw, 10, 64)
	if e != nil || recipient <= 0 || recipient == owner {
		return LedgerRequest{}, ErrInvalid
	}
	amount := o.Amount/10000*rate + o.Amount%10000*rate/10000
	if amount <= 0 {
		return LedgerRequest{}, ErrInvalid
	}
	source, e := s.orderIncomeSource(ctx, tx, o)
	if e != nil {
		return LedgerRequest{}, e
	}
	if amount > source.Remaining {
		return LedgerRequest{}, ErrInsufficient
	}

	v, e := s.requestFundsTx(ctx, tx, "profitsharing", owner, recipient, fmt.Sprintf("order:%s:rule:%d", tradeNo, ruleID), amount, actor)
	if e != nil {
		return v, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO order_profit_allocations(order_id,rule_id,request_id,amount,rate_bps) VALUES($1,$2,$3,$4,$5)", o.ID, ruleID, v.ID, amount, rate); e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
