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

// FundsAvailability returns one coherent transaction snapshot. Reservation callers
// must use requireOutgoingFunds instead, so a concurrent debit cannot reuse it.
func (s *Store) FundsAvailability(ctx context.Context, merchantID int64) (FundsAvailability, error) {
	return s.readFundsAvailability(ctx, s.pool, merchantID, nil)
}

func (s *Store) readFundsAvailability(ctx context.Context, q fundsReader, merchantID int64, at *time.Time) (FundsAvailability, error) {
	var v FundsAvailability
	if merchantID <= 0 {
		return v, ErrInvalid
	}
	tx, ok := q.(pgx.Tx)
	if !ok {
		var e error
		tx, e = s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if e != nil {
			return v, e
		}
		defer tx.Rollback(ctx)
		v, e = s.readFundsAvailability(ctx, tx, merchantID, at)
		if e != nil {
			return v, e
		}
		return v, tx.Commit(ctx)
	}
	var mode string
	var start time.Time
	e := tx.QueryRow(ctx, `SELECT a.balance,COALESCE((SELECT COALESCE(body->>'value','') FROM backoffice_records WHERE resource='settings' AND body->>'key'='settle_type'),'0'),((COALESCE($2::timestamptz,statement_timestamp()) AT TIME ZONE 'Asia/Shanghai')::date::timestamp AT TIME ZONE 'Asia/Shanghai') FROM accounts a WHERE a.owner_id=$1 AND a.kind='available'`, merchantID, at).Scan(&v.LedgerAvailable, &mode, &start)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if e != nil {
		return v, e
	}
	v.Available = v.LedgerAvailable
	if mode == "0" {
		return v, nil
	}
	if mode != "1" {
		return v, ErrInvalid
	}
	v.SettleType = 1
	rows, e := tx.Query(ctx, `SELECT o.id,order_income_channel_sharing_outflow(o.id),COALESCE((SELECT sum(r.amount) FROM refunds r WHERE r.order_id=o.id AND r.status='succeeded'),0)::bigint FROM orders o WHERE o.merchant_id=$1 AND o.status='paid' AND o.paid_at>=$2
 AND NOT EXISTS(SELECT 1 FROM order_income_holds h WHERE h.order_id=o.id AND h.state='held')
 AND NOT EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=o.id AND NOT EXISTS(SELECT 1 FROM channel_sharing_fund_releases c WHERE c.order_id=f.order_id))
 AND NOT EXISTS(SELECT 1 FROM recharge_intents i WHERE i.order_id=o.id) ORDER BY o.id LIMIT 100001`, merchantID, start)
	if e != nil {
		return v, e
	}
	type candidate struct{ id, outflow, refunded int64 }
	items := []candidate{}
	ids := []int64{}
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.id, &c.outflow, &c.refunded); e != nil {
			rows.Close()
			return v, e
		}
		items = append(items, c)
		ids = append(ids, c.id)
		if len(ids) > 100000 {
			rows.Close()
			return v, ErrState
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	charges, e := s.orderChargesTx104(ctx, tx, merchantID, ids)
	if e != nil {
		return v, e
	}
	if len(charges) != len(ids) {
		return v, ErrConflict
	}
	for _, c := range items {
		gross := charges[c.id].Charge
		if c.outflow < 0 || c.refunded < 0 || c.refunded > gross {
			return v, ErrConflict
		}
		if c.refunded == gross {
			continue
		}
		gross -= c.outflow
		if gross < 0 {
			gross = 0
		}
		if v.TodayGross > 9223372036854775807-gross {
			v.TodayGross = 9223372036854775807
		} else {
			v.TodayGross += gross
		}
	}
	v.Blocked = v.TodayGross
	if v.Blocked > v.LedgerAvailable {
		v.Blocked = v.LedgerAvailable
	}
	v.Available = v.LedgerAvailable - v.Blocked
	return v, nil
}

// Lock only accounts used by an outgoing reservation, in post's canonical order.
// Calculate in a subsequent statement: a SELECT which waited for a row lock can
// otherwise retain an older snapshot for its order/refund aggregate. Do not lock
// order rows here: payment/refund completion already locks order then accounts.
func (s *Store) requireOutgoingFunds(ctx context.Context, tx pgx.Tx, merchantID, amount int64) error {
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
	v, e := s.readFundsAvailability(ctx, tx, merchantID, nil)
	if e != nil {
		return e
	}
	if amount > v.Available {
		return ErrInsufficient
	}
	return nil
}
