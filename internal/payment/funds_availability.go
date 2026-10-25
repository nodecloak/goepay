package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

// FundsAvailability is a view over the immutable ledger, not another balance.
// TodayGross is saturated at MaxInt64; Blocked is capped at LedgerAvailable.
type FundsAvailability struct {
	LedgerAvailable int64
	Available       int64
	TodayGross      int64
	Blocked         int64
	SettleType      int
}

type fundsReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// FundsAvailability returns one coherent statement snapshot. Reservation callers
// must use requireOutgoingFunds instead, so a concurrent debit cannot reuse it.
func (s *Store) FundsAvailability(ctx context.Context, merchantID int64) (FundsAvailability, error) {
	return readFundsAvailability(ctx, s.pool, merchantID, nil)
}

func readFundsAvailability(ctx context.Context, q fundsReader, merchantID int64, at *time.Time) (FundsAvailability, error) {
	var v FundsAvailability
	if merchantID <= 0 {
		return v, ErrInvalid
	}
	var mode string
	e := q.QueryRow(ctx, `WITH policy AS MATERIALIZED (
	 SELECT COALESCE((SELECT COALESCE(body->>'value','') FROM backoffice_records WHERE resource='settings' AND body->>'key'='settle_type'),'0') AS mode,
	 ((COALESCE($2::timestamptz,statement_timestamp()) AT TIME ZONE 'Asia/Shanghai')::date::timestamp AT TIME ZONE 'Asia/Shanghai') AS day_start
	), gross AS (
	 SELECT COALESCE(sum(GREATEST(0::numeric,o.amount::numeric-order_income_channel_sharing_outflow(o.id))),0) AS amount FROM orders o CROSS JOIN policy p
	 WHERE p.mode='1' AND o.merchant_id=$1 AND o.status='paid' AND o.paid_at>=p.day_start
	 AND NOT EXISTS(SELECT 1 FROM order_income_holds h WHERE h.order_id=o.id AND h.state='held')
	 AND NOT EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=o.id AND NOT EXISTS(SELECT 1 FROM channel_sharing_fund_releases c WHERE c.order_id=f.order_id))
	 AND NOT EXISTS(SELECT 1 FROM recharge_intents i WHERE i.order_id=o.id)
	 AND COALESCE((SELECT sum(r.amount) FROM refunds r WHERE r.order_id=o.id AND r.status='succeeded'),0)<o.amount
	)
	SELECT a.balance, (a.balance::numeric-LEAST(a.balance::numeric,g.amount))::bigint,
	 LEAST(9223372036854775807::numeric,g.amount)::bigint,
	 LEAST(a.balance::numeric,g.amount)::bigint,p.mode
	FROM accounts a CROSS JOIN policy p CROSS JOIN gross g WHERE a.owner_id=$1 AND a.kind='available'`, merchantID, at).Scan(&v.LedgerAvailable, &v.Available, &v.TodayGross, &v.Blocked, &mode)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if e != nil {
		return v, e
	}
	switch mode {
	case "0":
	case "1":
		v.SettleType = 1
	default:
		return FundsAvailability{}, ErrInvalid
	}
	return v, nil
}

// Lock only accounts used by an outgoing reservation, in post's canonical order.
// Calculate in a subsequent statement: a SELECT which waited for a row lock can
// otherwise retain an older snapshot for its order/refund aggregate. Do not lock
// order rows here: payment/refund completion already locks order then accounts.
func requireOutgoingFunds(ctx context.Context, tx pgx.Tx, merchantID, amount int64) error {
	rows, e := tx.Query(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind IN ('available','reserved') ORDER BY id FOR UPDATE", merchantID)
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
	v, e := readFundsAvailability(ctx, tx, merchantID, nil)
	if e != nil {
		return e
	}
	if amount > v.Available {
		return ErrInsufficient
	}
	return nil
}
