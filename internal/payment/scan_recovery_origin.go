package payment

import (
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

type scanRecoveryIntent struct{ Provider string }

type scanRecoveryOrigin struct {
	AttemptID, OrderID, MerchantID, ChannelID                int64
	TradeNo, Provider                                        string
	Amount                                                   int64
	RequestIdentityHash, AccountScopeHash                    []byte
	Policy                                                   string
	MinimumWaitSeconds, QueryIntervalSeconds, MaximumQueries int
	CreatedAt, ExpiresAt                                     time.Time
	TransactionID                                            string
	ProofHash                                                []byte
}

const scanRecoveryOriginColumns = "attempt_id,order_id,merchant_id,channel_id,trade_no,provider,amount,request_identity_hash,account_scope_hash,policy,minimum_wait_seconds,query_interval_seconds,maximum_queries,created_at,expires_at,transaction_id,proof_hash"

// BeginRecoverableScanAttempt allocates only the original send opportunity and
// its immutable recovery terms. It performs no provider I/O and cannot backfill
// an existing scan. Pending-response eligibility and cancellation are separate.
func (s *Store) BeginRecoverableScanAttempt(ctx context.Context, tradeNo string, channelID int64, appID, authCode, accountScope string) (ProviderAttempt, bool, error) {
	return s.beginRecoverableScanAttempt(ctx, tradeNo, channelID, appID, authCode, accountScope, nil)
}

func (s *Store) beginRecoverableScanAttempt(ctx context.Context, tradeNo string, channelID int64, appID, authCode, accountScope string, sharing *sharingPaymentIntent) (ProviderAttempt, bool, error) {
	kind, e := adapter.ScanPaymentType(authCode)
	if e != nil || appID == "" || accountScope == "" {
		return ProviderAttempt{}, false, ErrInvalid
	}
	hash, e := s.providerIdentityHash(channelID, "scan", appID, authCode)
	if e != nil {
		return ProviderAttempt{}, false, e
	}
	if sharing == nil {
		sharing = &sharingPaymentIntent{ChannelSharingPaymentView: ChannelSharingPaymentView{AppID: appID}}
	}
	return s.beginProviderAttemptTerms(ctx, tradeNo, channelID, "scan", hash, accountScope, sharing, &scanRecoveryIntent{Provider: kind})
}

func (s *Store) scanRecoveryProof(v scanRecoveryOrigin) ([]byte, error) {
	return s.referralDigest("scan-recovery78/origin/v1", []any{v.AttemptID, v.OrderID, v.MerchantID, v.ChannelID, v.TradeNo, v.Provider, v.Amount, v.RequestIdentityHash, v.AccountScopeHash, v.Policy, v.MinimumWaitSeconds, v.QueryIntervalSeconds, v.MaximumQueries, v.CreatedAt.UTC().Format(time.RFC3339Nano), v.ExpiresAt.UTC().Format(time.RFC3339Nano), v.TransactionID})
}

func (s *Store) scanRecoveryAccountHash(id int64, scopeHash, sourceHash []byte) ([]byte, error) {
	if id <= 0 || len(scopeHash) != 32 || len(sourceHash) != 32 {
		return nil, ErrInvalid
	}
	// A V1 MAC can never validate as the legacy scope-only V0 MAC, even if an
	// imported row loses both version metadata and its original recovery source.
	return s.referralDigest("scan-recovery78/account/v1", []any{id, scopeHash, sourceHash})
}

func scanRecoveryAudit(v scanRecoveryOrigin) []byte {
	raw, _ := json.Marshal(map[string]string{"proof": hex.EncodeToString(v.ProofHash), "order_id": fmt.Sprint(v.OrderID)})
	return raw
}

func (s *Store) bindAttemptRecoveryAccount(ctx context.Context, tx pgx.Tx, id int64, scope string, created bool, recovery *scanRecoveryIntent) error {
	if recovery == nil {
		return s.bindProviderAccount(ctx, tx, id, scope, created)
	}
	if !created {
		if e := s.checkProviderAccountScope(ctx, tx, id, scope, true); e != nil {
			return e
		}
		// A valid V0 account does not imply permission to create a recovery origin.
		v, e := s.readScanRecoveryOrigin(ctx, tx, id)
		if e != nil {
			return e
		}
		if v.Provider != recovery.Provider {
			return ErrConflict
		}
		return nil
	}
	h, e := s.providerAccountHash(scope)
	if e != nil {
		return e
	}
	v := scanRecoveryOrigin{AttemptID: id, AccountScopeHash: h, Policy: "original-barcode-v1", MinimumWaitSeconds: 15, QueryIntervalSeconds: 3, MaximumQueries: 6}
	var enabled bool
	e = tx.QueryRow(ctx, `SELECT o.id,o.merchant_id,p.channel_id,o.trade_no,o.type,o.amount,i.request_hash,clock_timestamp(),txid_current()::text,m.enabled
 FROM provider_attempts p JOIN orders o ON o.id=p.order_id JOIN merchants m ON m.id=o.merchant_id JOIN provider_attempt_identities i ON i.attempt_id=p.id WHERE p.id=$1 AND p.method='scan'`, id).Scan(&v.OrderID, &v.MerchantID, &v.ChannelID, &v.TradeNo, &v.Provider, &v.Amount, &v.RequestIdentityHash, &v.CreatedAt, &v.TransactionID, &enabled)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrConflict
	}
	if e != nil {
		return e
	}
	if !enabled || v.Provider != recovery.Provider || len(v.RequestIdentityHash) != 32 {
		return ErrConflict
	}
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", v.OrderID))
	if e != nil {
		return e
	}
	v.Amount, e = s.authenticatedChargeValue104(ctx, tx, o)
	if e != nil {
		return e
	}
	v.ExpiresAt = v.CreatedAt.Add(120 * time.Second)
	v.ProofHash, e = s.scanRecoveryProof(v)
	if e != nil {
		return e
	}
	account, e := s.scanRecoveryAccountHash(id, h, v.ProofHash)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO scan_recovery_origins(`+scanRecoveryOriginColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, v.AttemptID, v.OrderID, v.MerchantID, v.ChannelID, v.TradeNo, v.Provider, v.Amount, v.RequestIdentityHash, v.AccountScopeHash, v.Policy, v.MinimumWaitSeconds, v.QueryIntervalSeconds, v.MaximumQueries, v.CreatedAt, v.ExpiresAt, v.TransactionID, v.ProofHash)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO provider_attempt_accounts(attempt_id,account_hash,recovery_version,recovery_source_hash)VALUES($1,$2,1,$3)`, id, account, v.ProofHash); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scan-recovery','scan-recovery-origins',$1,'create',$2)`, id, scanRecoveryAudit(v)); e != nil {
		return e
	}
	// Audit or database waits must not commit an already expired new authority.
	var current bool
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()<$1 AND enabled FROM merchants WHERE id=$2`, v.ExpiresAt, v.MerchantID).Scan(&current); e != nil {
		return e
	}
	if !current {
		return ErrConflict
	}
	return nil
}

func (s *Store) readScanRecoveryOrigin(ctx context.Context, q fundsReader, id int64) (scanRecoveryOrigin, error) {
	var v scanRecoveryOrigin
	e := q.QueryRow(ctx, "SELECT "+scanRecoveryOriginColumns+" FROM scan_recovery_origins WHERE attempt_id=$1", id).Scan(&v.AttemptID, &v.OrderID, &v.MerchantID, &v.ChannelID, &v.TradeNo, &v.Provider, &v.Amount, &v.RequestIdentityHash, &v.AccountScopeHash, &v.Policy, &v.MinimumWaitSeconds, &v.QueryIntervalSeconds, &v.MaximumQueries, &v.CreatedAt, &v.ExpiresAt, &v.TransactionID, &v.ProofHash)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, ErrConflict
	}
	if e != nil {
		return v, e
	}
	if v.AttemptID != id || id <= 0 || v.OrderID <= 0 || v.MerchantID <= 0 || v.ChannelID <= 0 || v.TradeNo == "" || v.Amount <= 0 || (v.Provider != "alipay" && v.Provider != "wxpay") || len(v.RequestIdentityHash) != 32 || len(v.AccountScopeHash) != 32 || len(v.ProofHash) != 32 || v.Policy != "original-barcode-v1" || v.MinimumWaitSeconds != 15 || v.QueryIntervalSeconds != 3 || v.MaximumQueries != 6 || v.CreatedAt.IsZero() || !v.ExpiresAt.Equal(v.CreatedAt.Add(120*time.Second)) || v.TransactionID == "" {
		return v, ErrConflict
	}
	proof, e := s.scanRecoveryProof(v)
	if e != nil || !hmac.Equal(proof, v.ProofHash) {
		return v, ErrConflict
	}
	reader, ok := q.(referralReader)
	if !ok {
		return v, ErrInvalid
	}
	o, e := scanOrder(q.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", v.OrderID))
	if e != nil {
		return v, e
	}
	charge, e := s.authenticatedChargeValue104(ctx, reader, o)
	if e != nil {
		return v, e
	}
	if charge != v.Amount {
		return v, ErrConflict
	}
	var original, audited bool
	e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_attempts p JOIN orders o ON o.id=p.order_id JOIN provider_attempt_identities i ON i.attempt_id=p.id WHERE p.id=$1 AND p.method='scan' AND p.channel_id=$2 AND (o.id,o.merchant_id,o.trade_no,o.type,COALESCE((to_jsonb(o)->>'actual_charge_amount104')::bigint,o.amount))=($3,$4,$5,$6,$7) AND i.request_hash=$8),
 (SELECT count(*)=1 AND count(*) FILTER(WHERE actor_role='system' AND actor_id=0 AND actor_name='scan-recovery' AND operation='create' AND changes=$9::jsonb)=1 FROM backoffice_audit WHERE resource='scan-recovery-origins' AND resource_id=$1)`, id, v.ChannelID, v.OrderID, v.MerchantID, v.TradeNo, v.Provider, v.Amount, v.RequestIdentityHash, scanRecoveryAudit(v)).Scan(&original, &audited)
	if e != nil {
		return v, e
	}
	if !original || !audited {
		return v, ErrConflict
	}
	// Expiration removes future cancellation permission, not the ability to read
	// and reconcile the original request. Never replace this with an expiry skip.
	return v, nil
}
