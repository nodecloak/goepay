package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

// ClaimScanCancellation commits the permanent single-send barrier under the
// same original order lock as every income path. It performs no provider I/O.
func (s *Store) ClaimScanCancellation(ctx context.Context, jobID int64, token, scope string) (ScanCancellationClaim, bool, error) {
	if jobID <= 0 || !validScanLease(token) || scope == "" {
		return ScanCancellationClaim{}, false, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ScanCancellationClaim{}, false, e
	}
	defer tx.Rollback(ctx)
	var attemptID int64
	e = tx.QueryRow(ctx, `SELECT p.id FROM jobs j JOIN provider_attempts p ON j.payload=jsonb_build_object('attempt_id',p.id::text) WHERE j.id=$1 AND j.kind='payment.scan-recover' AND j.event_key='scan-recover:'||p.id::text`, jobID).Scan(&attemptID)
	if errors.Is(e, pgx.ErrNoRows) {
		return ScanCancellationClaim{}, false, ErrConflict
	}
	if e != nil {
		return ScanCancellationClaim{}, false, e
	}
	status, e := lockScanRecoveryOrder(ctx, tx, attemptID)
	if e != nil {
		return ScanCancellationClaim{}, false, e
	}
	if e = lockScanRecoveryJob(ctx, tx, jobID, attemptID, token); e != nil {
		return ScanCancellationClaim{}, false, e
	}
	source, e := s.readScanRecoveryOrigin(ctx, tx, attemptID)
	if e != nil {
		return ScanCancellationClaim{}, false, e
	}
	if e = s.checkProviderAccountScope(ctx, tx, attemptID, scope, true); e != nil {
		return ScanCancellationClaim{}, false, e
	}
	eligible, e := s.readScanEligibility(ctx, tx, source)
	if e != nil {
		return ScanCancellationClaim{}, false, e
	}
	head, events, e := s.readScanQueryProgress(ctx, tx, source, eligible)
	if e != nil {
		return ScanCancellationClaim{}, false, e
	}
	old, e := s.readScanCancellation(ctx, tx, source, eligible, head, events)
	if e == nil {
		return scanCancellationView(old, source), false, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return ScanCancellationClaim{}, false, e
	}
	if status != "submitting" && status != "pending" && status != "unknown" {
		return ScanCancellationClaim{}, false, ErrState
	}
	if len(events) != 2*source.MaximumQueries {
		return ScanCancellationClaim{}, false, ErrState
	}
	last := events[len(events)-1]
	if last.Kind != "observed" || last.Receipt == nil || !scanQueryPending(source.Provider, last.Receipt.State) {
		return ScanCancellationClaim{}, false, ErrState
	}
	var closing bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM provider_operations WHERE order_id=$1 AND kind='close')", source.OrderID).Scan(&closing); e != nil {
		return ScanCancellationClaim{}, false, e
	}
	if closing {
		return ScanCancellationClaim{}, false, ErrConflict
	}
	leaseHash := sha256.Sum256([]byte(token))
	v := scanCancellationRecord{AttemptID: attemptID, JobID: jobID, SourceHash: source.ProofHash, EligibilityHash: eligible.ProofHash, QueryHeadHash: head.ProofHash, LeaseHash: leaseHash[:], Product: scanCancellationProduct(source.Provider), ProviderTradeNo: last.Receipt.ProviderTradeNo}
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp(),txid_current()::text").Scan(&v.CreatedAt, &v.TransactionID); e != nil {
		return ScanCancellationClaim{}, false, e
	}
	minimum := source.CreatedAt.Add(time.Duration(source.MinimumWaitSeconds) * time.Second)
	freshUntil := last.CreatedAt.Add(time.Duration(source.QueryIntervalSeconds) * time.Second)
	if v.CreatedAt.Before(minimum) || !v.CreatedAt.Before(source.ExpiresAt) || !v.CreatedAt.Before(freshUntil) {
		return ScanCancellationClaim{}, false, ErrState
	}
	v.ProofHash, e = s.scanCancellationProof(v)
	if e != nil {
		return ScanCancellationClaim{}, false, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO scan_cancellation_claims(attempt_id,job_id,source_hash,eligibility_hash,query_head_hash,lease_hash,product,provider_trade_no,created_at,transaction_id,proof_hash)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, v.AttemptID, v.JobID, v.SourceHash, v.EligibilityHash, v.QueryHeadHash, v.LeaseHash, v.Product, v.ProviderTradeNo, v.CreatedAt, v.TransactionID, v.ProofHash); e != nil {
		return ScanCancellationClaim{}, false, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scan-recovery','scan-cancellations',$1,'claim',$2)`, attemptID, scanCancellationAudit(v)); e != nil {
		return ScanCancellationClaim{}, false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE orders SET status='unknown' WHERE id=$1", source.OrderID); e != nil {
		return ScanCancellationClaim{}, false, e
	}
	var active bool
	e = tx.QueryRow(ctx, `SELECT j.state='running' AND j.lease_token=$2 AND j.lease_until>clock_timestamp() AND clock_timestamp()>=$3 AND clock_timestamp()<$4 AND clock_timestamp()<$5 AND m.enabled FROM jobs j JOIN merchants m ON m.id=$6 WHERE j.id=$1`, jobID, token, minimum, source.ExpiresAt, freshUntil, source.MerchantID).Scan(&active)
	if e != nil {
		return ScanCancellationClaim{}, false, e
	}
	if !active {
		return ScanCancellationClaim{}, false, ErrState
	}
	if e = tx.Commit(ctx); e != nil {
		return ScanCancellationClaim{}, false, e
	}
	return scanCancellationView(v, source), true, nil
}

// RecordScanCancellation persists the result of the one original send. The
// permanent original claim, rather than a renewable lease, authenticates a late
// response. Unknown results never close the order or release the income barrier.
func (s *Store) RecordScanCancellation(ctx context.Context, attemptID int64, token string, result adapter.ScanCancellationResult, cancelErr error) error {
	if attemptID <= 0 || !validScanLease(token) {
		return ErrInvalid
	}
	receipt, valid := adapter.VerifiedScanCancellation(result)
	if cancelErr == nil && !valid {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	status, e := lockScanRecoveryOrder(ctx, tx, attemptID)
	if e != nil {
		return e
	}
	source, e := s.readScanRecoveryOrigin(ctx, tx, attemptID)
	if e != nil {
		return e
	}
	if e = s.checkScanRecoveryAnchor(ctx, tx, source); e != nil {
		return e
	}
	eligible, e := s.readScanEligibility(ctx, tx, source)
	if e != nil {
		return e
	}
	head, events, e := s.readScanQueryProgress(ctx, tx, source, eligible)
	if e != nil {
		return e
	}
	claim, e := s.readScanCancellation(ctx, tx, source, eligible, head, events)
	if e != nil {
		return e
	}
	leaseHash := sha256.Sum256([]byte(token))
	if !hmac.Equal(leaseHash[:], claim.LeaseHash) {
		return ErrConflict
	}
	v := scanCancellationOutcome{AttemptID: attemptID, ClaimHash: claim.ProofHash, State: "unknown"}
	if cancelErr == nil {
		if !adapter.ScanCancellationMatchesRequest(result, adapter.ScanCancellationRequest{TradeNo: source.TradeNo, ProviderTradeNo: claim.ProviderTradeNo, Amount: source.Amount}) {
			return ErrConflict
		}
		if receipt.Provider != source.Provider || receipt.Product != claim.Product || receipt.TradeNo != source.TradeNo || receipt.Amount != source.Amount || claim.ProviderTradeNo != "" && receipt.ProviderTradeNo != claim.ProviderTradeNo {
			return ErrConflict
		}
		if e = s.checkProviderAccountScope(ctx, tx, attemptID, receipt.AccountScope, true); e != nil {
			return e
		}
		receipt.AccountScope = hex.EncodeToString(source.AccountScopeHash)
		requestHash, e := s.referralDigest("scan-recovery78/cancel-request", receipt.RequestSHA256)
		if e != nil {
			return e
		}
		receipt.RequestSHA256 = hex.EncodeToString(requestHash)
		v.State, v.Receipt = "cancelled", &receipt
	}
	old, e := s.readScanCancellationOutcome(ctx, tx, source, claim)
	if e == nil {
		if old.State != v.State || v.Receipt == nil && old.Receipt != nil || v.Receipt != nil && (old.Receipt == nil || *v.Receipt != *old.Receipt) {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(e, ErrNotFound) {
		return e
	}
	if status != "unknown" {
		return ErrConflict
	}
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp(),txid_current()::text").Scan(&v.CreatedAt, &v.TransactionID); e != nil {
		return e
	}
	v.ProofHash, e = s.scanCancellationOutcomeProof(v)
	if e != nil {
		return e
	}
	var raw []byte
	if v.Receipt != nil {
		raw, e = json.Marshal(v.Receipt)
		if e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO scan_cancellation_results(attempt_id,claim_hash,state,receipt,created_at,transaction_id,proof_hash)VALUES($1,$2,$3,$4,$5,$6,$7)`, attemptID, v.ClaimHash, v.State, raw, v.CreatedAt, v.TransactionID, v.ProofHash); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scan-recovery','scan-cancellation-results',$1,$2,$3)`, attemptID, v.State, scanCancellationOutcomeAudit(v)); e != nil {
		return e
	}
	if v.State == "cancelled" {
		if _, e = tx.Exec(ctx, "UPDATE orders SET status='closed' WHERE id=$1 AND status='unknown'", source.OrderID); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
