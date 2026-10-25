package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
)

type SettlementFunding struct {
	State          string `json:"state"`
	AccountID      int64  `json:"account_id,string"`
	GrossAmount    int64  `json:"gross_amount,string"`
	ReservedAmount int64  `json:"reserved_amount,string"`
}

func reserveSettlementFunding(ctx context.Context, tx pgx.Tx, v LedgerRequest) error {
	var account, available, journal int64
	if e := tx.QueryRow(ctx, "INSERT INTO accounts(owner_id,kind,scope_id) VALUES($1,'settlement_reserve',$2) RETURNING id", v.MerchantID, v.ID).Scan(&account); e != nil {
		return e
	}
	if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", v.MerchantID).Scan(&available); e != nil {
		return e
	}
	key := fmt.Sprintf("request:%d:reserve", v.ID)
	if e := postByAccountIDs(ctx, tx, key, "settlement dedicated reserve", available, account, v.Amount); e != nil {
		return e
	}
	if e := tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", key).Scan(&journal); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, "INSERT INTO settlement_fundings(request_id,account_id,gross_amount,reserve_journal_id) VALUES($1,$2,$3,$4)", v.ID, account, v.Amount, journal)
	return e
}
func rejectSettlementFunding(ctx context.Context, tx pgx.Tx, v LedgerRequest) (bool, error) {
	var account, available, amount int64
	e := tx.QueryRow(ctx, "SELECT account_id,gross_amount FROM settlement_fundings WHERE request_id=$1", v.ID).Scan(&account, &amount)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return true, e
	}
	if amount != v.Amount {
		return true, ErrConflict
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", v.MerchantID).Scan(&available); e != nil {
		return true, e
	}
	return true, postByAccountIDs(ctx, tx, fmt.Sprintf("request:%d:review", v.ID), "settlement rejected", account, available, amount)
}
func attachSettlementFunding(ctx context.Context, q fundsReader, v LedgerRequest) (LedgerRequest, error) {
	if v.Kind != "settlements" {
		return v, nil
	}
	f := SettlementFunding{State: "legacy_unscoped"}
	e := q.QueryRow(ctx, "SELECT f.account_id,f.gross_amount,a.balance FROM settlement_fundings f JOIN accounts a ON a.id=f.account_id WHERE f.request_id=$1", v.ID).Scan(&f.AccountID, &f.GrossAmount, &f.ReservedAmount)
	if errors.Is(e, pgx.ErrNoRows) {
		e = nil
	} else if e == nil {
		f.State = "reserved"
		if v.Status == "rejected" {
			f.State = "released"
		}
		var payoutState, bankState string
		var bank bool
		err := q.QueryRow(ctx, "SELECT t.status,COALESCE(b.state,''),bp.request_id IS NOT NULL FROM settlement_payouts p JOIN transfers t ON t.id=p.transfer_id LEFT JOIN settlement_bank_payouts bp ON bp.request_id=p.request_id LEFT JOIN bank_transfer_lifecycle b ON b.transfer_id=t.id WHERE p.request_id=$1", v.ID).Scan(&payoutState, &bankState, &bank)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return v, err
		}
		if payoutState == "succeeded" {
			f.State = "paid"
			if bank && f.ReservedAmount > 0 {
				f.State = "paid_fee_held"
			}
		} else if payoutState == "failed" {
			f.State = "released"
		}
		if bank && bankState == "reversed" {
			f.State = "returned"
		}
	}
	v.Funding = &f
	return v, e
}
