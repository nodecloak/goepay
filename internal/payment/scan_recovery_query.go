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

// ScanQueryClaim is a read opportunity under the original recovery task. It
// never authorizes a new barcode submission, cancellation, or balance credit.
type ScanQueryClaim struct {
	ID              int64  `json:"id,string"`
	AttemptID       int64  `json:"attempt_id,string"`
	JobID           int64  `json:"job_id,string"`
	Sequence        int    `json:"sequence"`
	ChannelID       int64  `json:"channel_id,string"`
	Provider        string `json:"provider"`
	TradeNo         string `json:"trade_no"`
	ProviderTradeNo string `json:"provider_trade_no"`
	Amount          int64  `json:"amount,string"`
}

type scanQueryEvent struct {
	ID, AttemptID      int64
	Revision, Sequence int
	Kind               string
	ClaimID, JobID     int64
	LeaseHash          []byte
	ProviderTradeNo    string
	PreviousHash       []byte
	Receipt            *adapter.ScanObservationReceipt
	CreatedAt          time.Time
	TransactionID      string
	ProofHash          []byte
}
type scanQueryHead struct {
	AttemptID            int64
	EligibilityHash      []byte
	Revision             int
	EventID              *int64
	EventHash, ProofHash []byte
}

func (s *Store) scanQueryHeadProof(h scanQueryHead) ([]byte, error) {
	return s.referralDigest("scan-recovery78/query-head", []any{h.AttemptID, h.EligibilityHash, h.Revision, h.EventID, h.EventHash})
}
func (s *Store) scanQueryEventProof(e scanQueryEvent) ([]byte, error) {
	return s.referralDigest("scan-recovery78/query-event", []any{e.ID, e.AttemptID, e.Revision, e.Sequence, e.Kind, e.ClaimID, e.JobID, e.LeaseHash, e.ProviderTradeNo, e.PreviousHash, e.Receipt, e.CreatedAt.UTC().Format(time.RFC3339Nano), e.TransactionID})
}
func scanQueryAudit(v scanQueryEvent) []byte {
	raw, _ := json.Marshal(map[string]string{"attempt_id": fmt.Sprint(v.AttemptID), "query_number": fmt.Sprint(v.Sequence), "kind": v.Kind, "proof": hex.EncodeToString(v.ProofHash)})
	return raw
}
func (s *Store) initScanQueryProgress(ctx context.Context, tx pgx.Tx, v scanRecoveryEligibility) error {
	h := scanQueryHead{AttemptID: v.AttemptID, EligibilityHash: v.ProofHash}
	proof, e := s.scanQueryHeadProof(h)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO scan_recovery_query_heads(attempt_id,eligibility_hash,revision,proof_hash)VALUES($1,$2,0,$3)`, h.AttemptID, h.EligibilityHash, proof)
	return e
}

func scanQueryPending(provider, state string) bool {
	return provider == "wxpay" && state == "USERPAYING" || provider == "alipay" && state == "WAIT_BUYER_PAY"
}
func scanInitialQueryDelay(source scanRecoveryOrigin, receipt adapter.ScanPendingReceipt) time.Duration {
	wait := time.Duration(source.QueryIntervalSeconds) * time.Second
	primary, _, _ := strings.Cut(receipt.Code, ":")
	if source.Provider == "wxpay" && receipt.Code == "USERPAYING" || source.Provider == "alipay" && primary == "10003" {
		wait += 2 * time.Second
	}
	return wait
}
func validStoredScanObservation(r adapter.ScanObservationReceipt, source scanRecoveryOrigin) bool {
	if r.Provider != source.Provider || r.TradeNo != source.TradeNo || r.Amount != source.Amount || r.Currency != "CNY" || r.AccountScope != hex.EncodeToString(source.AccountScopeHash) {
		return false
	}
	for _, digest := range []string{r.RequestSHA256, r.ResponseSHA256} {
		v, e := hex.DecodeString(digest)
		if e != nil || len(v) != 32 || hex.EncodeToString(v) != digest {
			return false
		}
	}
	if r.Provider == "wxpay" {
		return r.Product == "/pay/orderquery" && r.Authentication == "wechat_v2_md5_tls" && (r.State == "SUCCESS" || r.State == "REFUND" || r.State == "NOTPAY" || r.State == "CLOSED" || r.State == "REVOKED" || r.State == "USERPAYING" || r.State == "PAYERROR")
	}
	return r.Product == "alipay.trade.query" && r.Authentication == "alipay_rsa2_tls" && (r.State == "WAIT_BUYER_PAY" || r.State == "TRADE_SUCCESS" || r.State == "TRADE_FINISHED" || r.State == "TRADE_CLOSED")
}

func (s *Store) readScanQueryProgress(ctx context.Context, q referralReader, source scanRecoveryOrigin, eligible scanRecoveryEligibility) (scanQueryHead, []scanQueryEvent, error) {
	var h scanQueryHead
	e := q.QueryRow(ctx, `SELECT attempt_id,eligibility_hash,revision,event_id,event_hash,proof_hash FROM scan_recovery_query_heads WHERE attempt_id=$1`, source.AttemptID).Scan(&h.AttemptID, &h.EligibilityHash, &h.Revision, &h.EventID, &h.EventHash, &h.ProofHash)
	if errors.Is(e, pgx.ErrNoRows) {
		return h, nil, ErrConflict
	}
	if e != nil {
		return h, nil, e
	}
	proof, e := s.scanQueryHeadProof(h)
	if e != nil || h.AttemptID != source.AttemptID || !hmac.Equal(h.EligibilityHash, eligible.ProofHash) || h.Revision < 0 || h.Revision > 2*source.MaximumQueries || !hmac.Equal(proof, h.ProofHash) {
		return h, nil, ErrConflict
	}
	rows, e := q.Query(ctx, `SELECT id,attempt_id,revision,query_number,kind,query_claim_id,job_id,lease_hash,query_provider_no,previous_hash,receipt,created_at,transaction_id,proof_hash FROM scan_recovery_query_events WHERE attempt_id=$1 ORDER BY revision LIMIT 13`, source.AttemptID)
	if e != nil {
		return h, nil, e
	}
	var events []scanQueryEvent
	for rows.Next() {
		var v scanQueryEvent
		var raw []byte
		e = rows.Scan(&v.ID, &v.AttemptID, &v.Revision, &v.Sequence, &v.Kind, &v.ClaimID, &v.JobID, &v.LeaseHash, &v.ProviderTradeNo, &v.PreviousHash, &raw, &v.CreatedAt, &v.TransactionID, &v.ProofHash)
		if e != nil {
			break
		}
		if raw != nil {
			var receipt adapter.ScanObservationReceipt
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&receipt) != nil {
				e = ErrConflict
				break
			}
			v.Receipt = &receipt
		}
		events = append(events, v)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return h, nil, e
	}
	if len(events) != h.Revision {
		return h, nil, ErrConflict
	}
	var jobID int64
	if e = q.QueryRow(ctx, "SELECT id FROM jobs WHERE event_key=$1", scanRecoveryJobKey(source.AttemptID)).Scan(&jobID); e != nil {
		return h, nil, ErrConflict
	}
	previous := eligible.ProofHash
	knownNo := ""
	due := eligible.CreatedAt.Add(scanInitialQueryDelay(source, eligible.Receipt))
	for i, v := range events {
		if v.ID <= 0 || v.AttemptID != source.AttemptID || v.Revision != i+1 || v.Sequence != i/2+1 || v.JobID != jobID || len(v.LeaseHash) != 32 || v.TransactionID == "" || v.CreatedAt.IsZero() || !hmac.Equal(previous, v.PreviousHash) {
			return h, nil, ErrConflict
		}
		proof, e = s.scanQueryEventProof(v)
		if e != nil || !hmac.Equal(proof, v.ProofHash) {
			return h, nil, ErrConflict
		}
		if i%2 == 0 {
			if v.Kind != "query" || v.ClaimID != v.ID || v.Receipt != nil || v.ProviderTradeNo != knownNo || v.CreatedAt.Before(due) || !v.CreatedAt.Before(source.ExpiresAt) {
				return h, nil, ErrConflict
			}
			if i > 0 && (events[i-1].Receipt == nil || !scanQueryPending(source.Provider, events[i-1].Receipt.State)) {
				return h, nil, ErrConflict
			}
		} else {
			claim := events[i-1]
			if v.ClaimID != claim.ID || v.JobID != claim.JobID || !hmac.Equal(v.LeaseHash, claim.LeaseHash) || v.ProviderTradeNo != claim.ProviderTradeNo || v.CreatedAt.Before(claim.CreatedAt) {
				return h, nil, ErrConflict
			}
			if v.Kind == "observed" {
				if v.Receipt == nil || !validStoredScanObservation(*v.Receipt, source) || knownNo != "" && v.Receipt.ProviderTradeNo != knownNo {
					return h, nil, ErrConflict
				}
				if v.Receipt.ProviderTradeNo != "" {
					knownNo = v.Receipt.ProviderTradeNo
				}
			} else if v.Kind != "error" || v.Receipt != nil {
				return h, nil, ErrConflict
			}
			due = v.CreatedAt.Add(time.Duration(source.QueryIntervalSeconds) * time.Second)
		}
		var audited bool
		e = q.QueryRow(ctx, `SELECT count(*)=1 AND count(*) FILTER(WHERE actor_role='system' AND actor_id=0 AND actor_name='scan-recovery' AND operation=$2 AND changes=$3::jsonb)=1 FROM backoffice_audit WHERE resource='scan-recovery-queries' AND resource_id=$1`, v.ID, v.Kind, scanQueryAudit(v)).Scan(&audited)
		if e != nil {
			return h, nil, e
		}
		if !audited {
			return h, nil, ErrConflict
		}
		previous = v.ProofHash
	}
	var auditCount int
	e = q.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='scan-recovery-queries' AND changes->>'attempt_id'=$1::bigint::text`, source.AttemptID).Scan(&auditCount)
	if e != nil {
		return h, nil, e
	}
	if auditCount != len(events) {
		return h, nil, ErrConflict
	}
	if len(events) == 0 {
		if h.EventID != nil || h.EventHash != nil {
			return h, nil, ErrConflict
		}
	} else if h.EventID == nil || *h.EventID != events[len(events)-1].ID || !hmac.Equal(h.EventHash, previous) {
		return h, nil, ErrConflict
	}
	return h, events, nil
}

func (s *Store) checkScanRecoveryAnchor(ctx context.Context, q fundsReader, source scanRecoveryOrigin) error {
	var version int
	var stored, origin []byte
	e := q.QueryRow(ctx, "SELECT account_hash,recovery_version,recovery_source_hash FROM provider_attempt_accounts WHERE attempt_id=$1", source.AttemptID).Scan(&stored, &version, &origin)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrConflict
	}
	if e != nil {
		return e
	}
	expected, e := s.scanRecoveryAccountHash(source.AttemptID, source.AccountScopeHash, source.ProofHash)
	if e != nil || version != 1 || !hmac.Equal(origin, source.ProofHash) || !hmac.Equal(expected, stored) {
		return ErrConflict
	}
	return nil
}
