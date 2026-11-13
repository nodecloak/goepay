package cryptopay

import (
	"context"
	"fmt"
)

func (x *Executor) RunPending(ctx context.Context) error { return x.RunPendingFor(ctx, "") }
func (x *Executor) RunPendingFor(ctx context.Context, network string) error {
	// Persist a bounded rotation before external calls. Repeated failures and
	// unknown transactions must keep their original identity without monopolizing
	// every future batch. A crashed dispatcher only delays the same original
	// records until the next rotation; wallet locks still serialize execution.
	rows, e := x.Service.Pool.Query(ctx, `WITH batch AS (
 SELECT id FROM crypto_withdrawals
 WHERE ($1='' OR network=$1) AND state NOT IN ('requested','confirmed','failed','cancelled')
 ORDER BY last_dispatch_at,created_at,id LIMIT 100 FOR UPDATE SKIP LOCKED
 ) UPDATE crypto_withdrawals w SET last_dispatch_at=clock_timestamp()
 FROM batch WHERE w.id=batch.id RETURNING w.id`, network)
	if e != nil {
		return e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	var errs []error
	for _, id := range ids {
		if e = x.Execute(ctx, id); e != nil {
			errs = append(errs, fmt.Errorf("outgoing %s: %w", id, e))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d outgoing operations require reconciliation", len(errs))
	}
	return nil
}
