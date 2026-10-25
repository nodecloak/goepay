package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/platform/jobs"
)

// WaitScanRecovery awaits only the task created from the authenticated original
// charge. It receives no barcode and cannot allocate or resubmit a payment.
// A worker's active lease is observed rather than stolen by the HTTP caller.
func (g ProviderFlow) WaitScanRecovery(ctx context.Context, merchantID int64, tradeNo string) (Order, error) {
	if !g.ScanRecoveryEnabled || !g.AllowLive {
		return Order{}, ErrLiveDisabled
	}
	ctx, stop := context.WithTimeout(ctx, 23*time.Second)
	defer stop()
	o, a, _, e := g.owned(ctx, merchantID, tradeNo)
	if e != nil {
		return o, e
	}
	if o.Status == "paid" {
		return o, nil
	}
	source, e := g.Store.readScanRecoveryOrigin(ctx, g.Store.pool, a.ID)
	if e != nil {
		return o, e
	}
	if _, e = g.Store.readScanEligibility(ctx, g.Store.pool, source); e != nil {
		return o, e
	}
	q := jobs.Queue{Pool: g.Store.pool}
	for ctx.Err() == nil {
		current, e := g.Store.OrderByTradeNo(ctx, tradeNo)
		if e != nil {
			return o, e
		}
		o = current
		if o.Status == "paid" {
			return o, nil
		}
		if o.Status == "closed" {
			return o, ErrScanCancelled
		}
		var state string
		var due time.Time
		var attempts, maximum int
		e = g.Store.pool.QueryRow(ctx, `SELECT state,CASE WHEN state='running' THEN lease_until ELSE run_at END,attempts,max_attempts FROM jobs WHERE event_key=$1 AND kind='payment.scan-recover' AND payload=jsonb_build_object('attempt_id',$2::bigint::text)`, scanRecoveryJobKey(a.ID), a.ID).Scan(&state, &due, &attempts, &maximum)
		if errors.Is(e, pgx.ErrNoRows) {
			return o, ErrConflict
		}
		if e != nil {
			return o, e
		}
		if state == "succeeded" || state == "dead" || attempts >= maximum {
			return o, ErrScanPending
		}
		if state != "pending" && state != "running" {
			return o, ErrConflict
		}
		delay := time.Until(due)
		if delay <= 0 {
			job, e := g.Store.claimOriginalScanTask(ctx, a.ID)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return o, e
			}
			if e == nil {
				next, executeErr := g.ExecuteScanRecovery(ctx, job.ID, job.Token)
				finishBudget := 3 * time.Second
				if ctx.Err() != nil {
					//23 seconds for the request plus at most5 for the original
					// financial ACK and1 for lease bookkeeping stays below30.
					finishBudget = time.Second
				}
				finishCtx, finishStop := context.WithTimeout(context.WithoutCancel(ctx), finishBudget)
				if executeErr != nil {
					e = q.Retry(finishCtx, job, "original scan recovery requires reconciliation", 2*time.Second)
				} else if next > 0 {
					e = q.Retry(finishCtx, job, "original scan query interval", next)
				} else {
					e = q.Complete(finishCtx, job)
				}
				finishStop()
				if executeErr != nil || e != nil {
					return o, errors.Join(executeErr, e)
				}
				continue
			}
			delay = 250 * time.Millisecond
		}
		if delay > time.Second {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return o, errors.Join(ErrScanPending, ctx.Err())
		case <-timer.C:
		}
	}
	return o, errors.Join(ErrScanPending, ctx.Err())
}

func (s *Store) claimOriginalScanTask(ctx context.Context, attemptID int64) (jobs.Job, error) {
	var job jobs.Job
	nonce := make([]byte, 16)
	if _, e := rand.Read(nonce); e != nil {
		return job, e
	}
	token := hex.EncodeToString(nonce)
	e := s.pool.QueryRow(ctx, `WITH candidate AS(SELECT id FROM jobs WHERE event_key=$1 AND kind='payment.scan-recover' AND payload=jsonb_build_object('attempt_id',$2::bigint::text) AND attempts<max_attempts AND run_at<=clock_timestamp() AND(state='pending' OR(state='running' AND lease_until<clock_timestamp())) FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE jobs j SET state='running',attempts=attempts+1,lease_until=clock_timestamp()+interval '1 minute',lease_token=$3 FROM candidate c WHERE j.id=c.id RETURNING j.id,j.kind,j.payload,j.attempts,j.lease_token`, scanRecoveryJobKey(attemptID), attemptID, token).Scan(&job.ID, &job.Kind, &job.Payload, &job.Attempts, &job.Token)
	return job, e
}
