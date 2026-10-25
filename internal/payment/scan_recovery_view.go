package payment

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type ScanRecoveryView struct {
	State                  string     `json:"state"`
	Queries                int        `json:"queries"`
	MaximumQueries         int        `json:"maximum_queries"`
	LastProviderState      string     `json:"last_provider_state,omitempty"`
	NextQueryAt            *time.Time `json:"next_query_at,omitempty"`
	StartedAt              time.Time  `json:"started_at"`
	ExpiresAt              time.Time  `json:"expires_at"`
	RequiresReconciliation bool       `json:"requires_reconciliation"`
}

// ScanRecoveryStatus is a read-only console projection from verified original
// history. It never exposes bearer data, receipt hashes or sending authority.
// A legacy order has no recovery view and receives no inferred authorization.
func (s *Store) ScanRecoveryStatus(ctx context.Context, merchantID int64, tradeNo string) (*ScanRecoveryView, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var orderID int64
	var status string
	var now time.Time
	e = tx.QueryRow(ctx, "SELECT id,status,clock_timestamp() FROM orders WHERE merchant_id=$1 AND trade_no=$2", merchantID, tradeNo).Scan(&orderID, &status, &now)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	var tables int
	e = tx.QueryRow(ctx, `SELECT num_nonnulls(to_regclass('scan_recovery_origins'),to_regclass('scan_recovery_eligibility'),to_regclass('scan_recovery_query_events'),to_regclass('scan_recovery_query_heads'),to_regclass('scan_cancellation_claims'),to_regclass('scan_cancellation_results'))`).Scan(&tables)
	if e != nil {
		return nil, e
	}
	if tables == 0 {
		if e = s.checkScanCancellationBarrier(ctx, tx, orderID); e != nil {
			return nil, e
		}
		return nil, tx.Commit(ctx)
	}
	if tables != 6 {
		return nil, ErrConflict
	}
	var attemptID int64
	e = tx.QueryRow(ctx, "SELECT attempt_id FROM scan_recovery_origins WHERE order_id=$1", orderID).Scan(&attemptID)
	if errors.Is(e, pgx.ErrNoRows) {
		if e = s.checkScanCancellationBarrier(ctx, tx, orderID); e != nil {
			return nil, e
		}
		return nil, tx.Commit(ctx)
	}
	if e != nil {
		return nil, e
	}
	source, e := s.readScanRecoveryOrigin(ctx, tx, attemptID)
	if e != nil {
		return nil, e
	}
	if e = s.checkScanRecoveryAnchor(ctx, tx, source); e != nil {
		return nil, e
	}
	view := &ScanRecoveryView{State: "submitted", MaximumQueries: source.MaximumQueries, StartedAt: source.CreatedAt, ExpiresAt: source.ExpiresAt}
	eligible, e := s.readScanEligibility(ctx, tx, source)
	if errors.Is(e, ErrNotFound) {
		if e = s.scanCancellationBarrierForSource(ctx, tx, source); e != nil {
			return nil, e
		}
	} else if e != nil {
		return nil, e
	} else {
		head, events, e := s.readScanQueryProgress(ctx, tx, source, eligible)
		if e != nil {
			return nil, e
		}
		view.State = "waiting"
		next := eligible.CreatedAt.Add(scanInitialQueryDelay(source, eligible.Receipt))
		view.NextQueryAt = &next
		if len(events) > 0 {
			last := events[len(events)-1]
			view.Queries = last.Sequence
			view.NextQueryAt = nil
			if last.Kind == "query" || last.Kind == "error" {
				view.State = "query_unresolved"
				view.RequiresReconciliation = true
			} else if last.Receipt != nil {
				view.LastProviderState = last.Receipt.State
				if scanQueryPending(source.Provider, last.Receipt.State) {
					if last.Sequence < source.MaximumQueries {
						next = last.CreatedAt.Add(time.Duration(source.QueryIntervalSeconds) * time.Second)
						view.NextQueryAt = &next
					}
				} else {
					view.State = "stopped"
				}
			}
		}
		claim, e := s.readScanCancellation(ctx, tx, source, eligible, head, events)
		if e == nil {
			view.State = "cancel_pending"
			view.RequiresReconciliation = true
			view.NextQueryAt = nil
			outcome, e := s.readScanCancellationOutcome(ctx, tx, source, claim)
			if e == nil && outcome.State == "cancelled" {
				view.State = "cancelled"
				view.RequiresReconciliation = false
			} else if e != nil && !errors.Is(e, ErrNotFound) {
				return nil, e
			}
		} else if !errors.Is(e, ErrNotFound) {
			return nil, e
		}
	}
	if view.State != "cancel_pending" && view.State != "cancelled" {
		if status == "paid" {
			view.State = "completed"
			view.RequiresReconciliation = false
			view.NextQueryAt = nil
		} else if !now.Before(source.ExpiresAt) {
			view.State = "expired"
			view.RequiresReconciliation = true
			view.NextQueryAt = nil
		}
	}
	return view, tx.Commit(ctx)
}
