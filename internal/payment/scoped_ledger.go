package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

// postByAccountIDs supports explicitly bound scoped accounts. It shares the
// existing journal and balance rules; it never resolves a scope by pooled kind.
func postByAccountIDs(ctx context.Context, tx pgx.Tx, key, description string, fromID, toID, amount int64) error {
	if amount <= 0 || fromID <= 0 || toID <= 0 || fromID == toID {
		return ErrInvalid
	}
	rows, e := tx.Query(ctx, "SELECT id FROM accounts WHERE id=ANY($1::bigint[]) ORDER BY id FOR UPDATE", []int64{fromID, toID})
	if e != nil {
		return e
	}
	count := 0
	for rows.Next() {
		count++
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if count != 2 {
		return ErrNotFound
	}
	var journalID int64
	e = tx.QueryRow(ctx, "INSERT INTO journals(event_key,description) VALUES($1,$2) ON CONFLICT DO NOTHING RETURNING id", key, description).Scan(&journalID)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, "UPDATE accounts SET balance=balance-$1 WHERE id=$2 AND ((owner_id=0 AND kind IN ('clearing','bank_fee_expense')) OR balance>=$1)", amount, fromID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrInsufficient
	}
	if _, e = tx.Exec(ctx, "UPDATE accounts SET balance=balance+$1 WHERE id=$2", amount, toID); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO ledger_entries(journal_id,account_id,amount) VALUES($1,$2,-$4::bigint),($1,$3,$4::bigint)", journalID, fromID, toID, amount)
	return e
}
