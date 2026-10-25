package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

type scanRecoveryEligibility struct {
	AttemptID     int64
	SourceHash    []byte
	Receipt       adapter.ScanPendingReceipt
	CreatedAt     time.Time
	TransactionID string
	ProofHash     []byte
}

func (s *Store) scanEligibilityProof(v scanRecoveryEligibility) ([]byte, error) {
	return s.referralDigest("scan-recovery78/eligibility/v1", []any{v.AttemptID, v.SourceHash, v.Receipt, v.CreatedAt.UTC().Format(time.RFC3339Nano), v.TransactionID})
}
func scanEligibilityAudit(v scanRecoveryEligibility) []byte {
	raw, _ := json.Marshal(map[string]string{"proof": hex.EncodeToString(v.ProofHash), "source": hex.EncodeToString(v.SourceHash)})
	return raw
}
func scanRecoveryJobKey(id int64) string { return fmt.Sprintf("scan-recover:%d", id) }

// RecordScanPending accepts only a private authenticated response to the exact
// original in-memory barcode request. It never sends, cancels, or credits money.
// The original request and its bearer code are discarded after keyed binding.
func (s *Store) RecordScanPending(ctx context.Context, id int64, appID string, r adapter.ScanRequest, pending error) error {
	receipt, valid := adapter.VerifiedScanPending(pending)
	if id <= 0 || appID == "" || len(appID) > 256 || !valid || !adapter.ScanPendingMatchesRequest(pending, r) {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var orderID int64
	if e = tx.QueryRow(ctx, "SELECT order_id FROM provider_attempts WHERE id=$1", id).Scan(&orderID); errors.Is(e, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	var status string
	if e = tx.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1 FOR UPDATE", orderID).Scan(&status); e != nil {
		return e
	}
	source, e := s.readScanRecoveryOrigin(ctx, tx, id)
	if e != nil {
		return e
	}
	identity, e := s.providerIdentityHash(source.ChannelID, "scan", appID, r.AuthCode)
	if e != nil {
		return e
	}
	if !hmac.Equal(identity, source.RequestIdentityHash) || receipt.TradeNo != source.TradeNo || receipt.Provider != source.Provider || receipt.Amount != source.Amount {
		return ErrConflict
	}
	declaration, e := scanSharingPayment(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingPaymentColumns+" FROM provider_attempt_sharing WHERE attempt_id=$1", id))
	if e != nil {
		return e
	}
	if e = s.verifySharingPayment(declaration); e != nil {
		return e
	}
	if declaration.Declared != r.ProfitSharing {
		return ErrConflict
	}
	if e = s.checkProviderAccountScope(ctx, tx, id, receipt.AccountScope, true); e != nil {
		return e
	}
	// Database receipts contain keyed derivatives instead of a plaintext account
	// scope or an unkeyed hash of the bearer request.
	receipt.AccountScope = hex.EncodeToString(source.AccountScopeHash)
	requestHash, e := s.referralDigest("scan-recovery78/receipt-request", receipt.RequestSHA256)
	if e != nil {
		return e
	}
	receipt.RequestSHA256 = hex.EncodeToString(requestHash)
	old, e := s.readScanEligibility(ctx, tx, source)
	if e == nil {
		if old.Receipt != receipt {
			return ErrConflict
		}
		_, _, e = s.readScanQueryProgress(ctx, tx, source, old)
		return e
	}
	if !errors.Is(e, ErrNotFound) {
		return e
	}
	if status != "submitting" && status != "pending" && status != "unknown" {
		return ErrState
	}
	v := scanRecoveryEligibility{AttemptID: id, SourceHash: source.ProofHash, Receipt: receipt}
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp(),txid_current()::text").Scan(&v.CreatedAt, &v.TransactionID); e != nil {
		return e
	}
	if v.CreatedAt.Before(source.CreatedAt) || !v.CreatedAt.Before(source.ExpiresAt) {
		return ErrState
	}
	v.ProofHash, e = s.scanEligibilityProof(v)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(v.Receipt)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO scan_recovery_eligibility(attempt_id,source_hash,receipt,created_at,transaction_id,proof_hash)VALUES($1,$2,$3,$4,$5,$6)`, id, v.SourceHash, raw, v.CreatedAt, v.TransactionID, v.ProofHash); e != nil {
		return e
	}
	if e = s.initScanQueryProgress(ctx, tx, v); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scan-recovery','scan-recovery-eligibility',$1,'eligible',$2)`, id, scanEligibilityAudit(v)); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload,run_at)VALUES($1,'payment.scan-recover',jsonb_build_object('attempt_id',$2::bigint::text),$3)`, scanRecoveryJobKey(id), id, v.CreatedAt.Add(scanInitialQueryDelay(source, v.Receipt))); e != nil {
		return e
	}
	var active bool
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()<$1 AND enabled FROM merchants WHERE id=$2", source.ExpiresAt, source.MerchantID).Scan(&active); e != nil {
		return e
	}
	if !active {
		return ErrState
	}
	return tx.Commit(ctx)
}

func validStoredScanPending(r adapter.ScanPendingReceipt, source scanRecoveryOrigin) bool {
	if r.Provider != source.Provider || r.TradeNo != source.TradeNo || r.Amount != source.Amount || r.AccountScope != hex.EncodeToString(source.AccountScopeHash) {
		return false
	}
	for _, digest := range []string{r.RequestSHA256, r.ResponseSHA256} {
		v, e := hex.DecodeString(digest)
		if e != nil || len(v) != 32 || hex.EncodeToString(v) != digest {
			return false
		}
	}
	if r.Provider == "wxpay" {
		return r.Product == "/pay/micropay" && r.Authentication == "wechat_v2_md5" && (r.Code == "USERPAYING" || r.Code == "SYSTEMERROR")
	}
	primary, _, _ := strings.Cut(r.Code, ":")
	return r.Product == "alipay.trade.pay" && r.Authentication == "alipay_rsa2" && (primary == "10003" || primary == "20000")
}

func (s *Store) readScanEligibility(ctx context.Context, q fundsReader, source scanRecoveryOrigin) (scanRecoveryEligibility, error) {
	var v scanRecoveryEligibility
	var raw []byte
	e := q.QueryRow(ctx, `SELECT attempt_id,source_hash,receipt,created_at,transaction_id,proof_hash FROM scan_recovery_eligibility WHERE attempt_id=$1`, source.AttemptID).Scan(&v.AttemptID, &v.SourceHash, &raw, &v.CreatedAt, &v.TransactionID, &v.ProofHash)
	if errors.Is(e, pgx.ErrNoRows) {
		var orphan bool
		e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='scan-recovery-eligibility' AND resource_id=$1) OR EXISTS(SELECT 1 FROM jobs WHERE event_key=$2)`, source.AttemptID, scanRecoveryJobKey(source.AttemptID)).Scan(&orphan)
		if e != nil {
			return v, e
		}
		if orphan {
			return v, ErrConflict
		}
		return v, ErrNotFound
	}
	if e != nil {
		return v, e
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&v.Receipt) != nil || !validStoredScanPending(v.Receipt, source) || v.AttemptID != source.AttemptID || !hmac.Equal(v.SourceHash, source.ProofHash) || v.CreatedAt.Before(source.CreatedAt) || !v.CreatedAt.Before(source.ExpiresAt) || v.TransactionID == "" {
		return v, ErrConflict
	}
	proof, e := s.scanEligibilityProof(v)
	if e != nil || !hmac.Equal(proof, v.ProofHash) {
		return v, ErrConflict
	}
	var audited, queued bool
	e = q.QueryRow(ctx, `SELECT (SELECT count(*)=1 AND count(*) FILTER(WHERE actor_role='system' AND actor_id=0 AND actor_name='scan-recovery' AND operation='eligible' AND changes=$2::jsonb)=1 FROM backoffice_audit WHERE resource='scan-recovery-eligibility' AND resource_id=$1),
 (SELECT count(*)=1 AND count(*) FILTER(WHERE kind='payment.scan-recover' AND payload=jsonb_build_object('attempt_id',$1::bigint::text))=1 FROM jobs WHERE event_key=$3)`, v.AttemptID, scanEligibilityAudit(v), scanRecoveryJobKey(v.AttemptID)).Scan(&audited, &queued)
	if e != nil {
		return v, e
	}
	if !audited || !queued {
		return v, ErrConflict
	}
	return v, nil
}
