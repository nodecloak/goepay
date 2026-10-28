package paymentdefend206

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"

	"github.com/jackc/pgx/v5"
)

// Continue is the exact context a create-order verification jump is bound to.
// A token minted for one context can never authorize a different merchant,
// order number, signed request, browser, policy revision, or verification mode.
type Continue struct {
	MerchantID        int64
	OutTradeNo        string
	Request           string // canonical identity of the verified signed request
	Browser           string // per-browser binding derived from the jump request
	PolicyFingerprint string // policy revision fingerprint at mint time
	Mode              string // pay_verify_type: "0" jump, "1" invisible, "2" slide
}

func validContinueMode(mode string) bool { return mode == "0" || mode == "1" || mode == "2" }

func (s *Store) continueBinding(c Continue) []byte {
	return s.sum("continue-binding", []any{c.MerchantID, c.OutTradeNo, c.Request, c.Browser, "create-order", c.PolicyFingerprint, c.Mode})
}

func (s *Store) continueProof(tokenHash, binding []byte, mode, fingerprint string) []byte {
	return s.sum("continue-proof", []any{hex.EncodeToString(tokenHash), hex.EncodeToString(binding), mode, fingerprint})
}

// ContinueMintTx mints a one-time continue token for the given verification
// context and persists it in the caller's transaction. No order, provider
// attempt, or send is created here. The deadline is set from the database
// clock so it never depends on a skewed host clock. Returns the opaque token
// the jump page must echo back; only its hash is stored.
func (s *Store) ContinueMintTx(ctx context.Context, tx pgx.Tx, c Continue, ttlSeconds int) (string, error) {
	if !s.valid() || tx == nil || !validContinueMode(c.Mode) || !hex64(c.PolicyFingerprint) || c.MerchantID <= 0 || c.OutTradeNo == "" || ttlSeconds <= 0 {
		return "", ErrInvalid
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	tokenHash := s.sum("continue-token", token)
	binding := s.continueBinding(c)
	proof := s.continueProof(tokenHash, binding, c.Mode, c.PolicyFingerprint)
	if _, err := tx.Exec(ctx, `INSERT INTO payment_defend206_continue(token_hash,binding_hash,purpose,mode,policy_fingerprint,expires_at,proof_hash)
 VALUES($1,$2,'create-order',$3,$4,clock_timestamp()+make_interval(secs=>$5),$6)`, tokenHash, binding, c.Mode, c.PolicyFingerprint, ttlSeconds, proof); err != nil {
		return "", err
	}
	return token, nil
}

// ContinueConsumeTx verifies and consumes a continue token exactly once inside
// the caller's order-creation transaction. It fails closed on an unknown,
// already-consumed, expired, tampered, or mismatched token. Success means the
// token matched this exact verification context and was atomically marked
// consumed; the caller may then create the order and make the first send.
func (s *Store) ContinueConsumeTx(ctx context.Context, tx pgx.Tx, token string, c Continue) error {
	if !s.valid() || tx == nil || token == "" || !validContinueMode(c.Mode) {
		return ErrInvalid
	}
	tokenHash := s.sum("continue-token", token)
	var binding, proof []byte
	var mode, fingerprint string
	var consumed *string
	err := tx.QueryRow(ctx, `SELECT binding_hash,mode,policy_fingerprint,consumed_at::text,proof_hash
 FROM payment_defend206_continue WHERE token_hash=$1 AND purpose='create-order' FOR UPDATE`, tokenHash).Scan(&binding, &mode, &fingerprint, &consumed, &proof)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrInvalid
		}
		return err
	}
	if consumed != nil {
		return ErrConflict
	}
	if !hmac.Equal(proof, s.continueProof(tokenHash, binding, mode, fingerprint)) {
		return ErrConflict
	}
	if mode != c.Mode || fingerprint != c.PolicyFingerprint || !hmac.Equal(binding, s.continueBinding(c)) {
		return ErrInvalid
	}
	// One-shot consume with a database-clock deadline check. A replay, an
	// expired token, or a concurrent winner leaves RowsAffected()!=1.
	tag, err := tx.Exec(ctx, `UPDATE payment_defend206_continue SET consumed_at=clock_timestamp()
 WHERE token_hash=$1 AND consumed_at IS NULL AND expires_at>clock_timestamp()`, tokenHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrInvalid
	}
	return nil
}
