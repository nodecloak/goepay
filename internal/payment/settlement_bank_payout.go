package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// The caller holds merchant/request before transfer/bank locks. Settlement fee
// stays in the original scoped account until a verified return releases it.
func (s *Store) postSettlementBank(ctx context.Context, tx pgx.Tx, t Transfer, action string) (bool, error) {
	var id, mid, source, gross, net, fee, available, clearing int64
	var send string
	e := tx.QueryRow(ctx, `SELECT p.request_id,r.merchant_id,f.account_id,p.gross_amount,p.net_amount,p.fee_amount,p.send_state,
 av.id,cl.id FROM settlement_bank_payouts bp JOIN settlement_payouts p ON p.request_id=bp.request_id AND p.transfer_id=bp.transfer_id
 JOIN ledger_requests r ON r.id=p.request_id JOIN settlement_fundings f ON f.request_id=r.id
 JOIN accounts av ON av.owner_id=r.merchant_id AND av.kind='available' AND av.scope_id=0
 JOIN accounts cl ON cl.owner_id=0 AND cl.kind='clearing' AND cl.scope_id=0
 WHERE bp.transfer_id=$1 AND bp.bank_product=$2`, t.ID, t.BankProduct).Scan(&id, &mid, &source, &gross, &net, &fee, &send, &available, &clearing)
	if errors.Is(e, pgx.ErrNoRows) {
		// A malformed settlement must never fall back to the ordinary pool.
		if t.SettlementRequestID > 0 {
			return true, ErrState
		}
		return false, nil
	}
	if e != nil {
		return true, e
	}
	if send != "claimed" || mid != t.MerchantID || net != t.Amount || t.FeeAmount != 0 || t.CostAmount != net {
		return true, ErrState
	}
	switch action {
	case "principal":
		e = postByAccountIDs(ctx, tx, fmt.Sprintf("settlement-payout:%d:principal", id), "settlement bank net principal", source, clearing, net)
	case "return-settled":
		if e = lockLedgerAccountSet(ctx, tx, []int64{source, clearing, available}); e != nil {
			return true, e
		}
		e = postByAccountIDs(ctx, tx, fmt.Sprintf("transfer:%d:bank-return:settlement-principal", t.ID), "verified settlement bank principal return", clearing, available, net)
		if e == nil && fee > 0 {
			e = postByAccountIDs(ctx, tx, fmt.Sprintf("transfer:%d:bank-return:settlement-fee", t.ID), "verified settlement bank fee release", source, available, fee)
		}
	case "return-reserved":
		e = postByAccountIDs(ctx, tx, fmt.Sprintf("settlement-payout:%d:release", id), "verified settlement bank reservation return", source, available, gross)
	default:
		e = ErrInvalid
	}
	return true, e
}
