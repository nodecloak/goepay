package payment

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateMerchantAtomic(ctx context.Context, m Merchant, after func(context.Context, pgx.Tx) error) error {
	if s.pool == nil || !s.validMerchantCreation(m) {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = s.CreateMerchantInTx(ctx, tx, m); e != nil {
		return e
	}
	if after != nil {
		if e = after(ctx, tx); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

// CreateMerchantInTx uses the caller's transaction so a verified registration
// can consume its OTP and invitation atomically with the zero-balance accounts.
func (s *Store) CreateMerchantInTx(ctx context.Context, tx pgx.Tx, m Merchant) error {
	if tx == nil || !s.validMerchantCreation(m) {
		return ErrInvalid
	}
	if m.SignatureAlgorithm == "" {
		m.SignatureAlgorithm = "MD5"
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		return e
	}
	secret := s.aead.Seal(nonce, nonce, []byte(m.APIKey), []byte(fmt.Sprint(m.ID)))
	_, e := tx.Exec(ctx, "INSERT INTO merchants(id,name,api_key,rsa_public_key,signature_algorithm) VALUES($1,$2,$3,$4,$5)", m.ID, m.Name, secret, m.RSAPublicKey, m.SignatureAlgorithm)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO accounts(owner_id,kind) VALUES($1,'available'),($1,'reserved')", m.ID)
	if e != nil {
		return e
	}
	return nil
}
func (s *Store) validMerchantCreation(m Merchant) bool {
	return m.ID > 0 && m.Name != "" && len(m.APIKey) >= 8 && s.aead != nil &&
		(m.SignatureAlgorithm == "" || m.SignatureAlgorithm == "MD5" || m.SignatureAlgorithm == "RSA" && m.RSAPublicKey != "")
}

// ReplaceMerchantAtomic changes configuration without touching any money account.
// API secrets remain encrypted with merchant-bound associated data.
func (s *Store) ReplaceMerchantAtomic(ctx context.Context, m Merchant, profile []byte, after func(context.Context, pgx.Tx) error) error {
	if m.ID <= 0 || m.Name == "" || len(m.APIKey) < 8 || s.aead == nil || (m.SignatureAlgorithm != "MD5" && m.SignatureAlgorithm != "RSA") {
		return ErrInvalid
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		return e
	}
	secret := s.aead.Seal(nonce, nonce, []byte(m.APIKey), []byte(fmt.Sprint(m.ID)))
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, "UPDATE merchants SET name=$2,enabled=$3,api_key=$4,signature_algorithm=$5,rsa_public_key=$6,profile=$7::jsonb,config_revision=config_revision+1 WHERE id=$1 AND config_revision=$8", m.ID, m.Name, m.Enabled, secret, m.SignatureAlgorithm, m.RSAPublicKey, string(profile), m.ConfigRevision)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if after != nil {
		if e = after(ctx, tx); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
