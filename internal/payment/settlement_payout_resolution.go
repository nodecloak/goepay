package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// Every common resolver enters a settlement in the same order as its claim.
// This happens before taking the transfer lock, including single/worker query.
func (s *Store) lockSettlementTransfer(ctx context.Context, tx pgx.Tx, tid int64) (int64, error) {
	var id int64
	e := tx.QueryRow(ctx, "SELECT request_id FROM settlement_payouts WHERE transfer_id=$1", tid).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, nil
	}
	if e != nil {
		return 0, e
	}
	_, _, e = s.lockSettlementRequest(ctx, tx, id)
	if e == nil {
		e = s.verifySettlementAutoPayout(ctx, tx, id)
	}
	return id, e
}
func (s *Store) resolveSettlementTransferTx(ctx context.Context, tx pgx.Tx, tid int64, state string) (bool, error) {
	id, e := s.lockSettlementTransfer(ctx, tx, tid)
	if e != nil {
		return true, e
	}
	if id == 0 {
		return false, nil
	}
	var mid, account, gross, net, fee, available, clearing, fees int64
	var send, current string
	e = tx.QueryRow(ctx, "SELECT r.merchant_id,f.account_id,p.gross_amount,p.net_amount,p.fee_amount,p.send_state,t.status FROM settlement_payouts p JOIN ledger_requests r ON r.id=p.request_id JOIN settlement_fundings f ON f.request_id=p.request_id JOIN transfers t ON t.id=p.transfer_id WHERE p.request_id=$1 AND t.id=$2 FOR UPDATE OF t,p", id, tid).Scan(&mid, &account, &gross, &net, &fee, &send, &current)
	if e != nil {
		return true, e
	}
	if send != "claimed" && !(send == "cancelled" && state == "failed") {
		return true, ErrState
	}
	if send == "cancelled" {
		var proof bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM settlement_payout_cancellations WHERE request_id=$1 AND transfer_id=$2)", id, tid).Scan(&proof); e != nil {
			return true, e
		}
		if !proof {
			return true, ErrState
		}
	}
	if current == state {
		return true, nil
	}
	if current == "succeeded" || current == "failed" {
		return true, ErrState
	}
	switch state {
	case "pending", "unknown":
	case "failed":
		if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", mid).Scan(&available); e != nil {
			return true, e
		}
		e = postByAccountIDs(ctx, tx, fmt.Sprintf("settlement-payout:%d:release", id), "settlement payout released", account, available, gross)
	case "succeeded":
		if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0").Scan(&clearing); e != nil {
			return true, e
		}
		ids := []int64{account, clearing}
		if fee > 0 {
			if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=0 AND kind='fees' AND scope_id=0").Scan(&fees); e != nil {
				return true, e
			}
			ids = append(ids, fees)
		}
		if e = lockLedgerAccountSet(ctx, tx, ids); e != nil {
			return true, e
		}
		if e = postByAccountIDs(ctx, tx, fmt.Sprintf("settlement-payout:%d:principal", id), "settlement payout net principal", account, clearing, net); e != nil {
			return true, e
		}
		if fee > 0 {
			e = postByAccountIDs(ctx, tx, fmt.Sprintf("settlement-payout:%d:fee", id), "settlement application fee", account, fees, fee)
		}
	default:
		return true, ErrInvalid
	}
	if e != nil {
		return true, e
	}
	_, e = tx.Exec(ctx, "UPDATE transfers SET status=$2,updated_at=clock_timestamp() WHERE id=$1", tid, state)
	return true, e
}
