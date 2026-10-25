package payment

import (
	"context"
	"errors"
)

// Called after taking the original order lock and before any receipt, journal,
// reward, printing source, or normal close operation is written. This check is
// unconditional even when new recovery execution has been turned off.
func (s *Store) checkScanCancellationBarrier(ctx context.Context, q referralReader, orderID int64) error {
	var attemptID int64
	var version int
	var present, accountsPresent, originsPresent bool
	e := q.QueryRow(ctx, `SELECT to_regclass('scan_cancellation_claims') IS NOT NULL,to_regclass('provider_attempt_accounts') IS NOT NULL,to_regclass('scan_recovery_origins') IS NOT NULL`).Scan(&present, &accountsPresent, &originsPresent)
	if e != nil {
		return e
	}
	if !accountsPresent {
		if present || originsPresent {
			return ErrConflict
		}
		return nil
	}
	if present != originsPresent {
		return ErrConflict
	}
	e = q.QueryRow(ctx, `SELECT COALESCE((SELECT id FROM provider_attempts WHERE order_id=$1),0),COALESCE((SELECT (to_jsonb(a)->>'recovery_version')::int FROM provider_attempt_accounts a JOIN provider_attempts p ON p.id=a.attempt_id WHERE p.order_id=$1),0)`, orderID).Scan(&attemptID, &version)
	if e != nil {
		return e
	}
	if !present {
		if version != 0 {
			return ErrConflict
		}
		return nil
	}
	var originated bool
	e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scan_recovery_origins WHERE order_id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='scan-recovery-origins' AND changes->>'order_id'=$1::bigint::text)`, orderID).Scan(&originated)
	if e != nil {
		return e
	}
	if !originated {
		if version != 0 {
			return ErrConflict
		}
		return nil
	}
	if version != 1 || attemptID <= 0 {
		return ErrConflict
	}
	source, e := s.readScanRecoveryOrigin(ctx, q, attemptID)
	if e != nil {
		return e
	}
	if e = s.checkScanRecoveryAnchor(ctx, q, source); e != nil {
		return e
	}
	return s.scanCancellationBarrierForSource(ctx, q, source)
}

// The caller has already authenticated the original source and versioned
// account. Reuse these same checks for income and the read-only ledger audit.
func (s *Store) scanCancellationBarrierForSource(ctx context.Context, q referralReader, source scanRecoveryOrigin) error {
	attemptID := source.AttemptID
	eligible, e := s.readScanEligibility(ctx, q, source)
	if errors.Is(e, ErrNotFound) {
		var orphan bool
		e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scan_cancellation_claims WHERE attempt_id=$1) OR EXISTS(SELECT 1 FROM scan_cancellation_results WHERE attempt_id=$1) OR EXISTS(SELECT 1 FROM scan_recovery_query_heads WHERE attempt_id=$1) OR EXISTS(SELECT 1 FROM scan_recovery_query_events WHERE attempt_id=$1)
   OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource IN('scan-cancellations','scan-cancellation-results') AND resource_id=$1 OR resource='scan-recovery-queries' AND changes->>'attempt_id'=$1::bigint::text)`, attemptID).Scan(&orphan)
		if e != nil {
			return e
		}
		if orphan {
			return ErrConflict
		}
		return nil
	}
	if e != nil {
		return e
	}
	head, events, e := s.readScanQueryProgress(ctx, q, source, eligible)
	if e != nil {
		return e
	}
	claim, e := s.readScanCancellation(ctx, q, source, eligible, head, events)
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	outcome, e := s.readScanCancellationOutcome(ctx, q, source, claim)
	if errors.Is(e, ErrNotFound) {
		return ErrScanCancellationPending
	}
	if e != nil {
		return e
	}
	if outcome.State == "cancelled" {
		return ErrScanCancelled
	}
	return ErrScanCancellationPending
}
