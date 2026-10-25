package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

func (s *Store) transferAccountHash(scope string) ([]byte, error) {
	if len(s.indexKey) != 32 || scope == "" || len(scope) > 4096 {
		return nil, ErrInvalid
	}
	m := hmac.New(sha256.New, s.indexKey)
	m.Write([]byte("goepay/transfer-account/v1\x00"))
	m.Write([]byte(scope))
	return m.Sum(nil), nil
}
func (s *Store) bindTransferAccount(ctx context.Context, tx pgx.Tx, id int64, scope string) error {
	h, e := s.transferAccountHash(scope)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO provider_transfer_accounts(transfer_id,account_hash) VALUES($1,$2)", id, h)
	return e
}
func (s *Store) checkTransferAccountScope(ctx context.Context, q fundsReader, id int64, scope string, strict bool) error {
	h, e := s.transferAccountHash(scope)
	if e != nil {
		return e
	}
	var old []byte
	e = q.QueryRow(ctx, "SELECT account_hash FROM provider_transfer_accounts WHERE transfer_id=$1", id).Scan(&old)
	if errors.Is(e, pgx.ErrNoRows) {
		if strict {
			return ErrConflict
		}
		return nil
	}
	if e != nil {
		return e
	}
	if !hmac.Equal(h, old) {
		return ErrConflict
	}
	return nil
}
func (s *Store) checkTransferAccount(ctx context.Context, q fundsReader, id int64, p adapter.Provider) error {
	scope, e := adapter.TransferAccountScope(p)
	if e != nil {
		return e
	}
	_, strict := p.(adapter.TransferAccountIdentity)
	return s.checkTransferAccountScope(ctx, q, id, scope, strict)
}
