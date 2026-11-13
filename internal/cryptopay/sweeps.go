package cryptopay

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strconv"
)

// RunSweeps consumes an indexed durable queue. A completed empty address never
// occupies a polling slot, and new deposits monotonically advance its revision.
func (x *Executor) RunSweeps(ctx context.Context, network string) error {
	rows, e := x.Service.Pool.Query(ctx, "SELECT address,revision FROM crypto_sweep_queue WHERE network=$1 AND next_attempt<=clock_timestamp() ORDER BY next_attempt,address LIMIT 100", network)
	if e != nil {
		return e
	}
	type item struct {
		address  string
		revision int64
	}
	var items []item
	for rows.Next() {
		var i item
		if e = rows.Scan(&i.address, &i.revision); e != nil {
			rows.Close()
			return e
		}
		items = append(items, i)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, i := range items {
		_, e = x.RequestSweep(ctx, network, i.address, fmt.Sprintf("sweep-%s-%d", i.address, i.revision))
		next := "clock_timestamp()+interval '30 seconds'"
		if e == nil {
			next = "'infinity'::timestamptz"
		}
		if _, e = x.Service.Pool.Exec(ctx, "UPDATE crypto_sweep_queue SET next_attempt="+next+" WHERE network=$1 AND address=$2 AND revision=$3", network, i.address, i.revision); e != nil {
			return e
		}
	}
	return nil
}
func retrySweep(ctx context.Context, tx pgx.Tx, v Withdrawal) error {
	_, e := tx.Exec(ctx, "UPDATE crypto_sweep_queue SET failures=failures+1,revision=revision+1,next_attempt=CASE WHEN failures+1<3 THEN clock_timestamp()+interval '30 seconds' ELSE 'infinity'::timestamptz END WHERE network=$1 AND address=$2", v.Network, v.Source)
	return e
}
func (s *Service) ResumeSweep(ctx context.Context, network, address string, actor int64) error {
	a, e := normalize(network, address)
	if e != nil || actor <= 0 {
		return ErrInvalid
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var invoice string
	e = tx.QueryRow(ctx, "SELECT id FROM crypto_invoices WHERE network=$1 AND address=$2 FOR UPDATE", network, a).Scan(&invoice)
	if e != nil {
		return ErrNotFound
	}
	var active bool
	e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM crypto_withdrawals WHERE network=$1 AND source=$2 AND state NOT IN ('confirmed','failed','cancelled'))", network, a).Scan(&active)
	if e != nil {
		return e
	}
	if active {
		return ErrConflict
	}
	tag, e := tx.Exec(ctx, "UPDATE crypto_sweep_queue SET failures=0,revision=revision+1,next_attempt=clock_timestamp() WHERE network=$1 AND address=$2", network, a)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if e = audit(ctx, tx, actor, "resume-sweep", invoice); e != nil {
		return e
	}
	return commit(ctx, tx)
}

func (x *Executor) RequestSweep(ctx context.Context, network, address, requestID string) (Withdrawal, error) {
	s := x.Service
	cfg := s.Networks[network]
	a := x.Adapters[network]
	if a == nil || !cfg.Enabled || !validKey(requestID) {
		return Withdrawal{}, ErrInvalid
	}
	source, e := normalize(network, address)
	if e != nil {
		return Withdrawal{}, e
	}
	destination, e := normalize(network, cfg.TreasuryAddress)
	if e != nil || source == destination {
		return Withdrawal{}, ErrInvalid
	}
	if e = a.VerifyNetwork(ctx); e != nil {
		return Withdrawal{}, e
	}
	raw, e := a.Balance(ctx, source)
	if e != nil {
		return Withdrawal{}, e
	}
	n, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || n <= 0 || n > maxAmount {
		return Withdrawal{}, ErrInvalid
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return Withdrawal{}, e
	}
	defer tx.Rollback(ctx)
	var merchant int64
	var inv string
	e = tx.QueryRow(ctx, "SELECT id,merchant_id FROM crypto_invoices WHERE network=$1 AND address=$2 FOR UPDATE", network, source).Scan(&inv, &merchant)
	if e != nil {
		return Withdrawal{}, ErrNotFound
	}
	old, e := scanWithdrawal(tx.QueryRow(ctx, "SELECT "+withdrawalColumns+" FROM crypto_withdrawals WHERE merchant_id=$1 AND request_id=$2", merchant, requestID))
	if e == nil {
		if old.Kind != "sweep" || old.Source != source || old.Address != destination {
			return Withdrawal{}, ErrConflict
		}
		return old, commit(ctx, tx)
	}
	if e != ErrNotFound {
		return Withdrawal{}, e
	}
	var active bool
	e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM crypto_withdrawals WHERE network=$1 AND source=$2 AND state NOT IN ('confirmed','failed','cancelled'))", network, source).Scan(&active)
	if e != nil {
		return Withdrawal{}, e
	}
	if active {
		return Withdrawal{}, ErrConflict
	}
	id := randomID()
	_, e = tx.Exec(ctx, "INSERT INTO crypto_withdrawals(id,merchant_id,request_id,kind,invoice_id,network,address,source,amount,state) VALUES($1,$2,$3,'sweep',$4,$5,$6,$7,$8,'approved')", id, merchant, requestID, inv, network, destination, source, n)
	if e != nil {
		return Withdrawal{}, e
	}
	v, e := scanWithdrawal(tx.QueryRow(ctx, "SELECT "+withdrawalColumns+" FROM crypto_withdrawals WHERE id=$1", id))
	if e != nil {
		return v, e
	}
	return v, commit(ctx, tx)
}
