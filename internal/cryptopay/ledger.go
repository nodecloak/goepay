package cryptopay

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
)

type posting struct {
	owner  int64
	kind   string
	amount int64
}

func post(ctx context.Context, tx pgx.Tx, network, ref string, rows ...posting) error {
	var total int64
	for _, r := range rows {
		total += r.amount
	}
	if total != 0 {
		return ErrInvalid
	}
	var id int64
	if e := tx.QueryRow(ctx, "INSERT INTO crypto_journals(network,reference) VALUES($1,$2) RETURNING id", network, ref).Scan(&id); e != nil {
		return e
	}
	for _, r := range rows {
		if r.amount == 0 {
			continue
		}
		var account int64
		e := tx.QueryRow(ctx, `INSERT INTO crypto_accounts(network,owner_id,kind) VALUES($1,$2,$3) ON CONFLICT(network,owner_id,kind) DO UPDATE SET kind=EXCLUDED.kind RETURNING id`, network, r.owner, r.kind).Scan(&account)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO crypto_entries(journal_id,account_id,amount) VALUES($1,$2,$3)", id, account, r.amount); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) Balances(ctx context.Context, merchant int64) ([]Balance, error) {
	if s == nil || s.Pool == nil {
		return nil, ErrUnavailable
	}
	rows, e := s.Pool.Query(ctx, `SELECT network,COALESCE(sum(balance) FILTER(WHERE kind='available'),0)::bigint,COALESCE(sum(balance) FILTER(WHERE kind='reserved'),0)::bigint FROM crypto_accounts WHERE (($1::bigint=0 AND owner_id>0) OR owner_id=$1) AND kind IN ('available','reserved') GROUP BY network ORDER BY network`, merchant)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Balance{}
	for rows.Next() {
		var v Balance
		var a, r int64
		if e = rows.Scan(&v.Network, &a, &r); e != nil {
			return nil, e
		}
		v.Available = FormatAmount(a)
		v.Reserved = FormatAmount(r)
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Service) Reconcile(ctx context.Context) (ReconcileResult, error) {
	var r ReconcileResult
	if s == nil || s.Pool == nil {
		return r, ErrUnavailable
	}
	e := s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM (SELECT j.id FROM crypto_journals j LEFT JOIN crypto_entries e ON e.journal_id=j.id GROUP BY j.id HAVING COALESCE(sum(e.amount),0)<>0 OR count(e.id)<2)t),(SELECT count(*) FROM crypto_accounts a WHERE a.balance<>(SELECT COALESCE(sum(e.amount),0) FROM crypto_entries e WHERE e.account_id=a.id)),(SELECT count(*) FROM crypto_accounts WHERE balance<0 AND NOT(owner_id=0 AND kind='clearing'))`).Scan(&r.ImbalancedJournals, &r.BalanceMismatches, &r.NegativeBalances)
	return r, e
}
func lockMerchant(ctx context.Context, tx pgx.Tx, id int64) error {
	var enabled bool
	e := tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR UPDATE", id).Scan(&enabled)
	if e != nil || !enabled {
		return ErrNotFound
	}
	return authorize(ctx, tx)
}
func audit(ctx context.Context, tx pgx.Tx, actor int64, op, id string) error {
	_, e := tx.Exec(ctx, "INSERT INTO crypto_audit(actor_id,operation,reference) VALUES($1,$2,$3)", actor, op, id)
	return e
}
func reserve(ctx context.Context, tx pgx.Tx, network string, owner, amount int64, ref string) error {
	var balance int64
	e := tx.QueryRow(ctx, "SELECT balance FROM crypto_accounts WHERE network=$1 AND owner_id=$2 AND kind='available' FOR UPDATE", network, owner).Scan(&balance)
	if e != nil || balance < amount {
		return ErrInsufficient
	}
	return post(ctx, tx, network, fmt.Sprintf("reserve:%s", ref), posting{owner, "available", -amount}, posting{owner, "reserved", amount})
}
