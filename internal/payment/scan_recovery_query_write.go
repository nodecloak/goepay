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

var ErrScanQueryNotDue = errors.New("original scan query interval has not elapsed")

func validScanLease(token string) bool {
	raw, e := hex.DecodeString(token)
	return e == nil && len(raw) == 16 && hex.EncodeToString(raw) == token
}
func scanQueryClaimView(v scanQueryEvent, source scanRecoveryOrigin) ScanQueryClaim {
	return ScanQueryClaim{ID: v.ClaimID, AttemptID: source.AttemptID, JobID: v.JobID, Sequence: v.Sequence, ChannelID: source.ChannelID, Provider: source.Provider, TradeNo: source.TradeNo, ProviderTradeNo: v.ProviderTradeNo, Amount: source.Amount}
}
func lockScanRecoveryOrder(ctx context.Context, tx pgx.Tx, attemptID int64) (string, error) {
	var status string
	e := tx.QueryRow(ctx, `SELECT o.status FROM orders o JOIN provider_attempts p ON p.order_id=o.id WHERE p.id=$1 FOR UPDATE OF o`, attemptID).Scan(&status)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return status, e
}
func lockScanRecoveryJob(ctx context.Context, tx pgx.Tx, jobID, attemptID int64, token string) error {
	var valid bool
	e := tx.QueryRow(ctx, `SELECT state='running' AND lease_token=$3 AND lease_until>clock_timestamp() AND kind='payment.scan-recover' AND event_key=$4 AND payload=jsonb_build_object('attempt_id',$2::bigint::text) FROM jobs WHERE id=$1 FOR UPDATE`, jobID, attemptID, token, scanRecoveryJobKey(attemptID)).Scan(&valid)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrConflict
	}
	if e != nil {
		return e
	}
	if !valid {
		return ErrConflict
	}
	return nil
}
func (s *Store) appendScanQueryEvent(ctx context.Context, tx pgx.Tx, head scanQueryHead, v scanQueryEvent) error {
	if v.Revision != head.Revision+1 {
		return ErrConflict
	}
	raw := []byte(nil)
	var e error
	if v.Receipt != nil {
		raw, e = json.Marshal(v.Receipt)
		if e != nil {
			return e
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO scan_recovery_query_events(id,attempt_id,revision,kind,query_number,query_claim_id,job_id,lease_hash,query_provider_no,previous_hash,receipt,created_at,transaction_id,proof_hash)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, v.ID, v.AttemptID, v.Revision, v.Kind, v.Sequence, v.ClaimID, v.JobID, v.LeaseHash, v.ProviderTradeNo, v.PreviousHash, raw, v.CreatedAt, v.TransactionID, v.ProofHash)
	if e != nil {
		return e
	}
	next := scanQueryHead{AttemptID: head.AttemptID, EligibilityHash: head.EligibilityHash, Revision: v.Revision, EventID: &v.ID, EventHash: v.ProofHash}
	next.ProofHash, e = s.scanQueryHeadProof(next)
	if e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `UPDATE scan_recovery_query_heads SET revision=$2,event_id=$3,event_hash=$4,proof_hash=$5 WHERE attempt_id=$1 AND revision=$6 AND proof_hash=$7`, next.AttemptID, next.Revision, next.EventID, next.EventHash, next.ProofHash, head.Revision, head.ProofHash)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	_, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scan-recovery','scan-recovery-queries',$1,$2,$3)`, v.ID, v.Kind, scanQueryAudit(v))
	return e
}
func newScanQueryEvent(ctx context.Context, tx pgx.Tx, head scanQueryHead) (scanQueryEvent, error) {
	v := scanQueryEvent{AttemptID: head.AttemptID, Revision: head.Revision + 1, PreviousHash: head.EventHash}
	if head.Revision == 0 {
		v.PreviousHash = head.EligibilityHash
	}
	e := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('scan_recovery_query_events','id')),clock_timestamp(),txid_current()::text`).Scan(&v.ID, &v.CreatedAt, &v.TransactionID)
	return v, e
}

// ClaimScanQuery permanently records one bounded read before network I/O. A
// replay of an unfinished claim returns owner=false and never grants it again.
func (s *Store) ClaimScanQuery(ctx context.Context, jobID int64, token, scope string) (ScanQueryClaim, bool, error) {
	if jobID <= 0 || !validScanLease(token) || scope == "" {
		return ScanQueryClaim{}, false, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ScanQueryClaim{}, false, e
	}
	defer tx.Rollback(ctx)
	var attemptID int64
	e = tx.QueryRow(ctx, `SELECT p.id FROM jobs j JOIN provider_attempts p ON j.payload=jsonb_build_object('attempt_id',p.id::text) WHERE j.id=$1 AND j.kind='payment.scan-recover' AND j.event_key='scan-recover:'||p.id::text`, jobID).Scan(&attemptID)
	if errors.Is(e, pgx.ErrNoRows) {
		return ScanQueryClaim{}, false, ErrConflict
	}
	if e != nil {
		return ScanQueryClaim{}, false, e
	}
	status, e := lockScanRecoveryOrder(ctx, tx, attemptID)
	if e != nil {
		return ScanQueryClaim{}, false, e
	}
	if e = lockScanRecoveryJob(ctx, tx, jobID, attemptID, token); e != nil {
		return ScanQueryClaim{}, false, e
	}
	source, e := s.readScanRecoveryOrigin(ctx, tx, attemptID)
	if e != nil {
		return ScanQueryClaim{}, false, e
	}
	if e = s.checkProviderAccountScope(ctx, tx, attemptID, scope, true); e != nil {
		return ScanQueryClaim{}, false, e
	}
	eligible, e := s.readScanEligibility(ctx, tx, source)
	if e != nil {
		return ScanQueryClaim{}, false, e
	}
	head, events, e := s.readScanQueryProgress(ctx, tx, source, eligible)
	if e != nil {
		return ScanQueryClaim{}, false, e
	}
	leaseHash := sha256.Sum256([]byte(token))
	if len(events) > 0 && events[len(events)-1].Kind == "query" {
		last := events[len(events)-1]
		if !hmac.Equal(last.LeaseHash, leaseHash[:]) {
			return ScanQueryClaim{}, false, ErrConflict
		}
		return scanQueryClaimView(last, source), false, nil
	}
	if status != "submitting" && status != "pending" && status != "unknown" {
		return ScanQueryClaim{}, false, ErrState
	}
	due := eligible.CreatedAt.Add(scanInitialQueryDelay(source, eligible.Receipt))
	number, providerNo := 1, ""
	if len(events) > 0 {
		last := events[len(events)-1]
		if last.Kind != "observed" || last.Receipt == nil || !scanQueryPending(source.Provider, last.Receipt.State) || last.Sequence >= source.MaximumQueries {
			return ScanQueryClaim{}, false, ErrState
		}
		due = last.CreatedAt.Add(time.Duration(source.QueryIntervalSeconds) * time.Second)
		number = last.Sequence + 1
		providerNo = last.Receipt.ProviderTradeNo
	}
	v, e := newScanQueryEvent(ctx, tx, head)
	if e != nil {
		return ScanQueryClaim{}, false, e
	}
	if v.CreatedAt.Before(due) {
		return ScanQueryClaim{}, false, ErrScanQueryNotDue
	}
	if !v.CreatedAt.Before(source.ExpiresAt) {
		return ScanQueryClaim{}, false, ErrState
	}
	v.Kind, v.Sequence, v.ClaimID, v.JobID, v.LeaseHash, v.ProviderTradeNo = "query", number, v.ID, jobID, leaseHash[:], providerNo
	v.ProofHash, e = s.scanQueryEventProof(v)
	if e != nil {
		return ScanQueryClaim{}, false, e
	}
	if e = s.appendScanQueryEvent(ctx, tx, head, v); e != nil {
		return ScanQueryClaim{}, false, e
	}
	var active bool
	e = tx.QueryRow(ctx, `SELECT j.state='running' AND j.lease_token=$2 AND j.lease_until>clock_timestamp() AND clock_timestamp()>=$3 AND clock_timestamp()<$4 AND m.enabled FROM jobs j JOIN merchants m ON m.id=$5 WHERE j.id=$1`, jobID, token, due, source.ExpiresAt, source.MerchantID).Scan(&active)
	if e != nil {
		return ScanQueryClaim{}, false, e
	}
	if !active {
		return ScanQueryClaim{}, false, ErrState
	}
	if e = tx.Commit(ctx); e != nil {
		return ScanQueryClaim{}, false, e
	}
	return scanQueryClaimView(v, source), true, nil
}

// RecordScanQueryResult consumes the exact private read receipt and original
// claim/lease. A read error ends this automatic chain without financial action.
func (s *Store) RecordScanQueryResult(ctx context.Context, claimID int64, token string, result adapter.QueryResult, queryErr error) error {
	if claimID <= 0 || !validScanLease(token) {
		return ErrInvalid
	}
	receipt, valid := adapter.VerifiedScanObservation(result)
	if queryErr == nil && !valid {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var attemptID, jobID int64
	e = tx.QueryRow(ctx, "SELECT attempt_id,job_id FROM scan_recovery_query_events WHERE id=$1 AND kind='query'", claimID).Scan(&attemptID, &jobID)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrConflict
	}
	if e != nil {
		return e
	}
	if _, e = lockScanRecoveryOrder(ctx, tx, attemptID); e != nil {
		return e
	}
	if e = lockScanRecoveryJob(ctx, tx, jobID, attemptID, token); e != nil {
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
	var claim scanQueryEvent
	for _, v := range events {
		if v.ID == claimID && v.Kind == "query" {
			claim = v
		}
	}
	hash := sha256.Sum256([]byte(token))
	if claim.ID == 0 || !hmac.Equal(claim.LeaseHash, hash[:]) {
		return ErrConflict
	}
	kind := "error"
	var saved *adapter.ScanObservationReceipt
	if queryErr == nil {
		if !adapter.ScanObservationMatchesRequest(result, adapter.QueryRequest{TradeNo: source.TradeNo, ProviderTradeNo: claim.ProviderTradeNo}) || receipt.Provider != source.Provider || receipt.Amount != source.Amount || receipt.Currency != "CNY" {
			return ErrConflict
		}
		if e = s.checkProviderAccountScope(ctx, tx, attemptID, receipt.AccountScope, true); e != nil {
			return e
		}
		receipt.AccountScope = hex.EncodeToString(source.AccountScopeHash)
		requestHash, e := s.referralDigest("scan-recovery78/query-request", receipt.RequestSHA256)
		if e != nil {
			return e
		}
		receipt.RequestSHA256 = hex.EncodeToString(requestHash)
		kind, saved = "observed", &receipt
	}
	last := events[len(events)-1]
	if last.ID != claimID {
		if last.ClaimID != claimID || last.Kind != kind || saved == nil && last.Receipt != nil || saved != nil && (last.Receipt == nil || *saved != *last.Receipt) {
			return ErrConflict
		}
		return nil
	}
	v, e := newScanQueryEvent(ctx, tx, head)
	if e != nil {
		return e
	}
	v.Kind, v.Sequence, v.ClaimID, v.JobID, v.LeaseHash, v.ProviderTradeNo, v.Receipt = kind, claim.Sequence, claim.ID, claim.JobID, claim.LeaseHash, claim.ProviderTradeNo, saved
	v.ProofHash, e = s.scanQueryEventProof(v)
	if e != nil {
		return e
	}
	if e = s.appendScanQueryEvent(ctx, tx, head, v); e != nil {
		return e
	}
	var active bool
	e = tx.QueryRow(ctx, "SELECT state='running' AND lease_token=$2 AND lease_until>clock_timestamp() FROM jobs WHERE id=$1", jobID, token).Scan(&active)
	if e != nil {
		return e
	}
	if !active {
		return ErrState
	}
	return tx.Commit(ctx)
}
