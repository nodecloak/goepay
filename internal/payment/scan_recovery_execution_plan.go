package payment

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type scanRecoveryExecutionPlan struct {
	source       scanRecoveryOrigin
	delay        time.Duration
	done, cancel bool
}

// This snapshot only selects the next step. The write claim rechecks its full
// authority under the original order lock before granting any network I/O.
func (s *Store) scanRecoveryExecutionPlan(ctx context.Context, jobID int64, token string) (scanRecoveryExecutionPlan, error) {
	var plan scanRecoveryExecutionPlan
	if jobID <= 0 || !validScanLease(token) {
		return plan, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return plan, e
	}
	defer tx.Rollback(ctx)
	var attemptID int64
	var status string
	var now time.Time
	e = tx.QueryRow(ctx, `SELECT a.id,o.status,clock_timestamp() FROM jobs j JOIN provider_attempts a ON j.payload=jsonb_build_object('attempt_id',a.id::text) JOIN orders o ON o.id=a.order_id WHERE j.id=$1 AND j.kind='payment.scan-recover' AND j.event_key='scan-recover:'||a.id::text AND j.state='running' AND j.lease_token=$2 AND j.lease_until>clock_timestamp()`, jobID, token).Scan(&attemptID, &status, &now)
	if errors.Is(e, pgx.ErrNoRows) {
		return plan, ErrConflict
	}
	if e != nil {
		return plan, e
	}
	plan.source, e = s.readScanRecoveryOrigin(ctx, tx, attemptID)
	if e != nil {
		return plan, e
	}
	if e = s.checkScanRecoveryAnchor(ctx, tx, plan.source); e != nil {
		return plan, e
	}
	eligible, e := s.readScanEligibility(ctx, tx, plan.source)
	if e != nil {
		return plan, e
	}
	head, events, e := s.readScanQueryProgress(ctx, tx, plan.source, eligible)
	if e != nil {
		return plan, e
	}
	_, e = s.readScanCancellation(ctx, tx, plan.source, eligible, head, events)
	if e == nil {
		// A permanent claim is never another send opportunity, including when
		// its first process crashed before saving the acknowledgement.
		if e = s.scanCancellationBarrierForSource(ctx, tx, plan.source); !errors.Is(e, ErrScanCancelled) && !errors.Is(e, ErrScanCancellationPending) {
			return plan, e
		}
		plan.done = true
	} else if !errors.Is(e, ErrNotFound) {
		return plan, e
	}
	if !plan.done {
		due := eligible.CreatedAt.Add(scanInitialQueryDelay(plan.source, eligible.Receipt))
		if len(events) > 0 {
			last := events[len(events)-1]
			if last.Kind != "observed" || last.Receipt == nil || !scanQueryPending(plan.source.Provider, last.Receipt.State) {
				plan.done = true
			} else if last.Sequence == plan.source.MaximumQueries {
				plan.cancel = true
			} else {
				due = last.CreatedAt.Add(time.Duration(plan.source.QueryIntervalSeconds) * time.Second)
			}
		}
		if status == "paid" || status == "closed" || !now.Before(plan.source.ExpiresAt) {
			plan.done = true
		}
		if !plan.done && !plan.cancel && now.Before(due) {
			plan.delay = due.Sub(now)
		}
	}
	return plan, tx.Commit(ctx)
}
