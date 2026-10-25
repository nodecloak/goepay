package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

var ErrScanCancellationPending = errors.New("original barcode cancellation outcome requires reconciliation")
var ErrScanCancelled = errors.New("original barcode payment has been cancelled")

type ScanCancellationClaim struct {
	AttemptID       int64  `json:"attempt_id,string"`
	OrderID         int64  `json:"order_id,string"`
	JobID           int64  `json:"job_id,string"`
	ChannelID       int64  `json:"channel_id,string"`
	Provider        string `json:"provider"`
	Product         string `json:"product"`
	TradeNo         string `json:"trade_no"`
	ProviderTradeNo string `json:"provider_trade_no"`
	Amount          int64  `json:"amount,string"`
}

type scanCancellationRecord struct {
	AttemptID, JobID                                      int64
	SourceHash, EligibilityHash, QueryHeadHash, LeaseHash []byte
	Product, ProviderTradeNo                              string
	CreatedAt                                             time.Time
	TransactionID                                         string
	ProofHash                                             []byte
}
type scanCancellationOutcome struct {
	AttemptID     int64
	ClaimHash     []byte
	State         string
	Receipt       *adapter.ScanCancellationReceipt
	CreatedAt     time.Time
	TransactionID string
	ProofHash     []byte
}

func scanCancellationProduct(provider string) string {
	if provider == "wxpay" {
		return "/secapi/pay/reverse"
	}
	return "alipay.trade.cancel"
}
func (s *Store) scanCancellationProof(v scanCancellationRecord) ([]byte, error) {
	return s.referralDigest("scan-recovery78/cancel-claim", []any{v.AttemptID, v.JobID, v.SourceHash, v.EligibilityHash, v.QueryHeadHash, v.LeaseHash, v.Product, v.ProviderTradeNo, v.CreatedAt.UTC().Format(time.RFC3339Nano), v.TransactionID})
}
func (s *Store) scanCancellationOutcomeProof(v scanCancellationOutcome) ([]byte, error) {
	return s.referralDigest("scan-recovery78/cancel-result", []any{v.AttemptID, v.ClaimHash, v.State, v.Receipt, v.CreatedAt.UTC().Format(time.RFC3339Nano), v.TransactionID})
}
func scanCancellationAudit(v scanCancellationRecord) []byte {
	raw, _ := json.Marshal(map[string]string{"attempt_id": fmt.Sprint(v.AttemptID), "proof": hex.EncodeToString(v.ProofHash), "query_head": hex.EncodeToString(v.QueryHeadHash)})
	return raw
}
func scanCancellationOutcomeAudit(v scanCancellationOutcome) []byte {
	raw, _ := json.Marshal(map[string]string{"attempt_id": fmt.Sprint(v.AttemptID), "proof": hex.EncodeToString(v.ProofHash), "claim": hex.EncodeToString(v.ClaimHash)})
	return raw
}
func scanCancellationView(v scanCancellationRecord, source scanRecoveryOrigin) ScanCancellationClaim {
	return ScanCancellationClaim{AttemptID: source.AttemptID, OrderID: source.OrderID, JobID: v.JobID, ChannelID: source.ChannelID, Provider: source.Provider, Product: v.Product, TradeNo: source.TradeNo, ProviderTradeNo: v.ProviderTradeNo, Amount: source.Amount}
}

func (s *Store) readScanCancellation(ctx context.Context, q fundsReader, source scanRecoveryOrigin, eligible scanRecoveryEligibility, head scanQueryHead, events []scanQueryEvent) (scanCancellationRecord, error) {
	var v scanCancellationRecord
	e := q.QueryRow(ctx, `SELECT attempt_id,job_id,source_hash,eligibility_hash,query_head_hash,lease_hash,product,provider_trade_no,created_at,transaction_id,proof_hash FROM scan_cancellation_claims WHERE attempt_id=$1`, source.AttemptID).Scan(&v.AttemptID, &v.JobID, &v.SourceHash, &v.EligibilityHash, &v.QueryHeadHash, &v.LeaseHash, &v.Product, &v.ProviderTradeNo, &v.CreatedAt, &v.TransactionID, &v.ProofHash)
	if errors.Is(e, pgx.ErrNoRows) {
		var orphan bool
		e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scan_cancellation_results WHERE attempt_id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource IN('scan-cancellations','scan-cancellation-results') AND resource_id=$1)`, source.AttemptID).Scan(&orphan)
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
	if len(events) != 2*source.MaximumQueries || head.Revision != len(events) {
		return v, ErrConflict
	}
	last := events[len(events)-1]
	if last.Kind != "observed" || last.Receipt == nil || !scanQueryPending(source.Provider, last.Receipt.State) || v.AttemptID != source.AttemptID || v.JobID != last.JobID || !hmac.Equal(v.SourceHash, source.ProofHash) || !hmac.Equal(v.EligibilityHash, eligible.ProofHash) || !hmac.Equal(v.QueryHeadHash, head.ProofHash) || len(v.LeaseHash) != 32 || v.Product != scanCancellationProduct(source.Provider) || v.ProviderTradeNo != last.Receipt.ProviderTradeNo || v.TransactionID == "" || v.CreatedAt.Before(source.CreatedAt.Add(time.Duration(source.MinimumWaitSeconds)*time.Second)) || !v.CreatedAt.Before(source.ExpiresAt) || v.CreatedAt.Before(last.CreatedAt) || !v.CreatedAt.Before(last.CreatedAt.Add(time.Duration(source.QueryIntervalSeconds)*time.Second)) {
		return v, ErrConflict
	}
	proof, e := s.scanCancellationProof(v)
	if e != nil || !hmac.Equal(proof, v.ProofHash) {
		return v, ErrConflict
	}
	var audited bool
	e = q.QueryRow(ctx, `SELECT count(*)=1 AND count(*) FILTER(WHERE actor_role='system' AND actor_id=0 AND actor_name='scan-recovery' AND operation='claim' AND changes=$2::jsonb)=1 FROM backoffice_audit WHERE resource='scan-cancellations' AND resource_id=$1`, v.AttemptID, scanCancellationAudit(v)).Scan(&audited)
	if e != nil {
		return v, e
	}
	if !audited {
		return v, ErrConflict
	}
	return v, nil
}

func validStoredScanCancellation(r adapter.ScanCancellationReceipt, source scanRecoveryOrigin, claim scanCancellationRecord) bool {
	if r.Provider != source.Provider || r.Product != claim.Product || r.TradeNo != source.TradeNo || r.Amount != source.Amount || r.State != "cancelled" || r.AccountScope != hex.EncodeToString(source.AccountScopeHash) || claim.ProviderTradeNo != "" && r.ProviderTradeNo != claim.ProviderTradeNo {
		return false
	}
	for _, digest := range []string{r.RequestSHA256, r.ResponseSHA256} {
		v, e := hex.DecodeString(digest)
		if e != nil || len(v) != 32 || hex.EncodeToString(v) != digest {
			return false
		}
	}
	if r.Provider == "wxpay" {
		return r.Authentication == "wechat_v2_md5_mtls" && r.Action == "close_or_refund"
	}
	return r.Authentication == "alipay_rsa2" && (r.Action == "close" || r.Action == "refund")
}

func (s *Store) readScanCancellationOutcome(ctx context.Context, q fundsReader, source scanRecoveryOrigin, claim scanCancellationRecord) (scanCancellationOutcome, error) {
	var v scanCancellationOutcome
	var raw []byte
	e := q.QueryRow(ctx, `SELECT attempt_id,claim_hash,state,receipt,created_at,transaction_id,proof_hash FROM scan_cancellation_results WHERE attempt_id=$1`, source.AttemptID).Scan(&v.AttemptID, &v.ClaimHash, &v.State, &raw, &v.CreatedAt, &v.TransactionID, &v.ProofHash)
	if errors.Is(e, pgx.ErrNoRows) {
		var orphan bool
		e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='scan-cancellation-results' AND resource_id=$1)", source.AttemptID).Scan(&orphan)
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
	if raw != nil {
		var receipt adapter.ScanCancellationReceipt
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&receipt) != nil {
			return v, ErrConflict
		}
		v.Receipt = &receipt
	}
	if v.AttemptID != source.AttemptID || !hmac.Equal(v.ClaimHash, claim.ProofHash) || v.TransactionID == "" || v.CreatedAt.Before(claim.CreatedAt) {
		return v, ErrConflict
	}
	if v.State == "cancelled" {
		if v.Receipt == nil || !validStoredScanCancellation(*v.Receipt, source, claim) {
			return v, ErrConflict
		}
	} else if v.State != "unknown" || v.Receipt != nil {
		return v, ErrConflict
	}
	proof, e := s.scanCancellationOutcomeProof(v)
	if e != nil || !hmac.Equal(proof, v.ProofHash) {
		return v, ErrConflict
	}
	var audited, stateMatches bool
	e = q.QueryRow(ctx, `SELECT (SELECT count(*)=1 AND count(*) FILTER(WHERE actor_role='system' AND actor_id=0 AND actor_name='scan-recovery' AND operation=$2 AND changes=$3::jsonb)=1 FROM backoffice_audit WHERE resource='scan-cancellation-results' AND resource_id=$1),
 (SELECT status=CASE WHEN $2::text='cancelled' THEN 'closed' ELSE 'unknown' END FROM orders WHERE id=$4)`, v.AttemptID, v.State, scanCancellationOutcomeAudit(v), source.OrderID).Scan(&audited, &stateMatches)
	if e != nil {
		return v, e
	}
	if !audited || !stateMatches {
		return v, ErrConflict
	}
	return v, nil
}
