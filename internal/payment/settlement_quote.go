package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type SettlementQuote struct {
	Policy    SettlementPolicy  `json:"policy"`
	Terms     *SettlementTerms  `json:"terms"`
	Funds     FundsAvailability `json:"-"`
	DailyUsed int64             `json:"daily_used,string"`
	CanApply  bool              `json:"can_apply"`
	Access    SettlementAccess  `json:"settlement_access"`
}

// A coherent read-only estimate; RequestFunds always rechecks under its locks.
func (s *Store) SettlementQuote(ctx context.Context, mid, amount int64) (SettlementQuote, error) {
	var out SettlementQuote
	if mid <= 0 || amount < 0 {
		return out, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", mid).Scan(&enabled); e != nil {
		return out, e
	}
	out.Policy, e = readSettlementPolicy(ctx, tx, false)
	if e != nil {
		return out, e
	}
	out.Access, e = readSettlementAccess(ctx, tx, mid, false, false)
	if e != nil {
		return out, e
	}
	out.Funds, e = readFundsAvailability(ctx, tx, mid, nil)
	if e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, "WITH day AS MATERIALIZED (SELECT ((clock_timestamp() AT TIME ZONE 'Asia/Shanghai')::date::timestamp AT TIME ZONE 'Asia/Shanghai') AS start) SELECT count(*) FROM ledger_requests CROSS JOIN day WHERE kind='settlements' AND merchant_id=$1 AND created_at>=day.start AND created_at<day.start+interval '24 hours'", mid).Scan(&out.DailyUsed); e != nil {
		return out, e
	}
	if amount > 0 {
		terms, err := out.Policy.Quote(amount)
		if err != nil {
			return out, err
		}
		out.Terms = &terms
		out.CanApply = enabled && out.Access.ManualAllowed && out.Funds.Available >= amount+out.Policy.RetainedCents && (out.Policy.DailyLimit == 0 || out.DailyUsed < out.Policy.DailyLimit)
	}
	return out, tx.Commit(ctx)
}
