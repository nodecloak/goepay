package paymentdefend206

import (
	"context"
	"crypto/hmac"
	"errors"

	"github.com/jackc/pgx/v5"
)

func validAdmissionKind(k string) bool { return k == "none" || k == "0" || k == "1" || k == "2" }

func (s *Store) admissionProof(merchantID int64, outTradeNo, kind, fingerprint string) []byte {
	return s.sum("admission", []any{merchantID, outTradeNo, kind, fingerprint})
}

// RecordAdmissionTx records, in the business transaction that passed the
// guard, that this merchant order number was admitted. The first record wins;
// a later admission of the same order number is already covered.
func (s *Store) RecordAdmissionTx(ctx context.Context, tx pgx.Tx, merchantID int64, outTradeNo, kind, fingerprint string) error {
	if !s.valid() || tx == nil || merchantID <= 0 || outTradeNo == "" || len(outTradeNo) > 128 || !validAdmissionKind(kind) || !hex64(fingerprint) {
		return ErrInvalid
	}
	_, err := tx.Exec(ctx, `INSERT INTO payment_defend206_admissions(merchant_id,out_trade_no,kind,policy_fingerprint,proof_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
		merchantID, outTradeNo, kind, fingerprint, s.admissionProof(merchantID, outTradeNo, kind, fingerprint))
	return err
}

// AdmittedTx reports whether the platform order has valid admission evidence
// under the given (current) policy revision: a policy changed since creation
// is never inherited by a continuation. A tampered record is a conflict.
func (s *Store) AdmittedTx(ctx context.Context, tx pgx.Tx, tradeNo, currentFingerprint string) (bool, error) {
	if !s.valid() || tx == nil || tradeNo == "" || !hex64(currentFingerprint) {
		return false, ErrInvalid
	}
	var merchantID int64
	var outTradeNo, kind, fingerprint string
	var proof []byte
	err := tx.QueryRow(ctx, `SELECT a.merchant_id,a.out_trade_no,a.kind,a.policy_fingerprint,a.proof_hash FROM orders o
 JOIN payment_defend206_admissions a ON a.merchant_id=o.merchant_id AND a.out_trade_no=o.out_trade_no WHERE o.trade_no=$1 FOR SHARE OF a`, tradeNo).Scan(&merchantID, &outTradeNo, &kind, &fingerprint, &proof)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !hmac.Equal(proof, s.admissionProof(merchantID, outTradeNo, kind, fingerprint)) {
		return false, ErrConflict
	}
	return fingerprint == currentFingerprint, nil
}

// AnyGroupOverrideTx reports whether any group override was ever configured.
func (s *Store) AnyGroupOverrideTx(ctx context.Context, tx pgx.Tx) (bool, error) {
	var any bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payment_defend206_groups) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource=$1)`, groupResource).Scan(&any)
	return any, err
}
