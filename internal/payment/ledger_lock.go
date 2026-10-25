package payment

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// New financial records reference merchant keys. Acquire those FK locks before
// account locks, so an INSERT cannot invert a deposit/membership transaction's
// merchant-then-wallet order. KEY SHARE preserves an existing entitlement even
// when the merchant was subsequently disabled.
func lockMerchantForeignKeySet(ctx context.Context, tx pgx.Tx, ids []int64) error {
	unique := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return ErrInvalid
		}
		unique[id] = struct{}{}
	}
	if len(unique) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, "SELECT id FROM merchants WHERE id=ANY($1::bigint[]) ORDER BY id FOR KEY SHARE", ids)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count != len(unique) {
		return ErrNotFound
	}
	return nil
}

// lockLedgerAccountSet takes the complete set before a transaction's first
// journal. Sorting each individual journal is insufficient when later journals
// introduce a lower account ID. Callers retain their domain locks first.
func lockLedgerAccountSet(ctx context.Context, tx pgx.Tx, ids []int64) error {
	unique := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return ErrInvalid
		}
		unique[id] = struct{}{}
	}
	if len(unique) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, "SELECT id FROM accounts WHERE id=ANY($1::bigint[]) ORDER BY id FOR UPDATE", ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if count != len(unique) {
		return ErrNotFound
	}
	return nil
}

// A bank result can post principal, held service fee and independently proved
// provider fee, or reverse those same accounts. Include the original dedicated
// settlement account before entering any of those branches.
func lockBankPostingAccounts(ctx context.Context, tx pgx.Tx, t Transfer) error {
	if err := lockMerchantForeignKeySet(ctx, tx, []int64{t.MerchantID}); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM accounts WHERE scope_id=0 AND
 ((owner_id=0 AND kind IN ('clearing','fees','bank_service_fee_hold','bank_fee_expense'))
 OR (owner_id=$1 AND kind IN ('available','reserved')))
 UNION SELECT f.account_id FROM settlement_payouts p JOIN settlement_fundings f ON f.request_id=p.request_id WHERE p.transfer_id=$2`, t.MerchantID, t.ID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	want := 6
	if t.SettlementRequestID > 0 {
		want++
	}
	if len(ids) != want {
		return ErrState
	}
	return lockLedgerAccountSet(ctx, tx, ids)
}
