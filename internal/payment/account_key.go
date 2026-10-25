package payment

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// RotateMerchantAPIKeyAtomic changes only encrypted API credentials, never
// overwriting concurrently changed merchant enablement, profile, or permissions.
func (s *Store) RotateMerchantAPIKeyAtomic(ctx context.Context, id int64, newKey string, authorize func(context.Context, pgx.Tx) error) error {
	if id <= 0 || len(newKey) < 24 || s.aead == nil || authorize == nil {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var algorithm string
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT signature_algorithm,enabled FROM merchants WHERE id=$1 FOR UPDATE", id).Scan(&algorithm, &enabled); e != nil {
		return e
	}
	if algorithm != "MD5" || !enabled {
		return ErrState
	}
	if e = authorize(ctx, tx); e != nil {
		return e
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	secret := s.aead.Seal(nonce, nonce, []byte(newKey), []byte(fmt.Sprint(id)))
	if _, e = tx.Exec(ctx, "UPDATE merchants SET api_key=$2 WHERE id=$1", id, secret); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
