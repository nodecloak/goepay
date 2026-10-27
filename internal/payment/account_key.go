package payment

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// RotateMerchantAPIKeyAtomic changes only encrypted API credentials, never
// overwriting concurrently changed merchant enablement, profile, or permissions.
func (s *Store) RotateMerchantAPIKeyAtomic(ctx context.Context, id int64, newKey string, authorize func(context.Context, pgx.Tx) error) error {
	if len(newKey) < 24 || authorize == nil {
		return ErrInvalid
	}
	_, e := s.rotateMerchantAPIKeyAtomic(ctx, id, func(ctx context.Context, tx pgx.Tx) (string, error) {
		if e := authorize(ctx, tx); e != nil {
			return "", e
		}
		return newKey, nil
	}, nil)
	return e
}

// RotateMerchantAPIKeyAtomicFinal keeps the same encrypted atomic MD5 write,
// generating a new secret only after authorization. The mandatory final hook
// runs after that write and any of its triggers, before committing. No secret
// is returned on a rejected, rolled back, or uncertain commit result.
func (s *Store) RotateMerchantAPIKeyAtomicFinal(ctx context.Context, id int64, authorize, final func(context.Context, pgx.Tx) error) (string, error) {
	if authorize == nil || final == nil {
		return "", ErrInvalid
	}
	return s.rotateMerchantAPIKeyAtomic(ctx, id, func(ctx context.Context, tx pgx.Tx) (string, error) {
		if e := authorize(ctx, tx); e != nil {
			return "", e
		}
		raw := make([]byte, 24)
		if _, e := rand.Read(raw); e != nil {
			return "", e
		}
		return base64.RawURLEncoding.EncodeToString(raw), nil
	}, final)
}

func (s *Store) rotateMerchantAPIKeyAtomic(ctx context.Context, id int64, prepare func(context.Context, pgx.Tx) (string, error), final func(context.Context, pgx.Tx) error) (string, error) {
	if s == nil || s.pool == nil || id <= 0 || s.aead == nil || prepare == nil {
		return "", ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var algorithm string
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT signature_algorithm,enabled FROM merchants WHERE id=$1 FOR UPDATE", id).Scan(&algorithm, &enabled); e != nil {
		return "", e
	}
	if algorithm != "MD5" || !enabled {
		return "", ErrState
	}
	newKey, e := prepare(ctx, tx)
	if e != nil {
		return "", e
	}
	if len(newKey) < 24 {
		return "", ErrInvalid
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	secret := s.aead.Seal(nonce, nonce, []byte(newKey), []byte(fmt.Sprint(id)))
	if _, e = tx.Exec(ctx, "UPDATE merchants SET api_key=$2 WHERE id=$1", id, secret); e != nil {
		return "", e
	}
	if final != nil {
		if e = final(ctx, tx); e != nil {
			return "", e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	return newKey, nil
}
