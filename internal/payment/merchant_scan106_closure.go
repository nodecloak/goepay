package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type merchantScanClosure106 struct {
	Version                     int
	MerchantID                  int64
	RequestID, IntentCommitment string
	ClosedAt                    time.Time
}

func scanIntentCommitment106(proof []byte) string {
	if len(proof) == 0 {
		return ""
	}
	return scanCommitment106(proof)
}

func (s *Store) readScanClosure106(ctx context.Context, q fundsReader, t MerchantScanIntent106, proof []byte) (bool, error) {
	var raw, mac []byte
	e := q.QueryRow(ctx, `SELECT facts,proof_hash FROM merchant_scan_closures106 WHERE merchant_id=$1 AND request_id=$2::uuid`, t.MerchantID, t.RequestID).Scan(&raw, &mac)
	var audits int
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return false, e
	}
	if ae := q.QueryRow(ctx, `SELECT count(*)FROM backoffice_audit WHERE resource='merchant-scan-closures106' AND actor_id=$1 AND changes->>'request_id'=$2`, t.MerchantID, t.RequestID).Scan(&audits); ae != nil {
		return false, ae
	}
	if errors.Is(e, pgx.ErrNoRows) {
		if audits != 0 {
			return false, ErrConflict
		}
		return false, nil
	}
	var facts merchantScanClosure106
	if len(raw) > 4096 || parentFactsJSON(raw, &facts) != nil || facts.Version != 1 || facts.MerchantID != t.MerchantID || facts.RequestID != t.RequestID || facts.IntentCommitment != scanIntentCommitment106(proof) || facts.ClosedAt.IsZero() || audits != 1 {
		return false, ErrConflict
	}
	want, e := s.referralDigest("merchant-scan106/closure", facts)
	if e != nil || !hmac.Equal(mac, want) {
		return false, ErrConflict
	}
	var matches bool
	if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='merchant-scan-closures106' AND actor_id=$1 AND changes->>'request_id'=$2 AND changes->>'commitment'=$3)`, t.MerchantID, t.RequestID, scanCommitment106(mac)).Scan(&matches); e != nil {
		return false, e
	}
	if !matches {
		return false, ErrConflict
	}
	return true, nil
}
func (s *Store) writeScanClosure106(ctx context.Context, tx pgx.Tx, a MerchantRSAActor, t MerchantScanIntent106, proof []byte) error {
	var at time.Time
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return e
	}
	facts := merchantScanClosure106{Version: 1, MerchantID: t.MerchantID, RequestID: t.RequestID, IntentCommitment: scanIntentCommitment106(proof), ClosedAt: at.UTC()}
	mac, e := s.referralDigest("merchant-scan106/closure", facts)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(facts)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO merchant_scan_closures106(merchant_id,request_id,facts,proof_hash)VALUES($1,$2::uuid,$3::jsonb,$4)`, t.MerchantID, t.RequestID, string(raw), mac); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('merchant',$1,$2,'merchant-scan-closures106',$1,'scan-close',jsonb_build_object('request_id',$3::text,'commitment',$4::text))`, a.User.ID, a.User.Username, t.RequestID, scanCommitment106(mac))
	return e
}
