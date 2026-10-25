package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

func (s *Store) providerAccountHash(scope string) ([]byte, error) {
	if len(s.indexKey) != 32 || scope == "" || len(scope) > 4096 {
		return nil, ErrInvalid
	}
	m := hmac.New(sha256.New, s.indexKey)
	m.Write([]byte("goepay/payment-account/v1\x00"))
	m.Write([]byte(scope))
	return m.Sum(nil), nil
}
func (s *Store) bindProviderAccount(ctx context.Context, tx pgx.Tx, id int64, scope string, created bool) error {
	if scope == "" {
		return nil
	} // Original internal entry points do not invent historical proof.
	h, e := s.providerAccountHash(scope)
	if e != nil {
		return e
	}
	if created {
		_, e = tx.Exec(ctx, "INSERT INTO provider_attempt_accounts(attempt_id,account_hash) VALUES($1,$2)", id, h)
		return e
	}
	return s.checkProviderAccountScope(ctx, tx, id, scope, true)
}
func (s *Store) checkProviderAccountScope(ctx context.Context, q fundsReader, id int64, scope string, strict bool) error {
	h, e := s.providerAccountHash(scope)
	if e != nil {
		return e
	}
	var old []byte
	var version int
	var source []byte
	// JSON projection also reads the actual pre78 shape without inventing a
	// missing recovery row or silently downgrading a new versioned account.
	e = q.QueryRow(ctx, "SELECT account_hash,COALESCE((to_jsonb(a)->>'recovery_version')::int,0),(to_jsonb(a)->>'recovery_source_hash')::bytea FROM provider_attempt_accounts a WHERE attempt_id=$1", id).Scan(&old, &version, &source)
	if errors.Is(e, pgx.ErrNoRows) {
		if strict {
			return ErrConflict
		}
		return nil
	}
	if e != nil {
		return e
	}
	if version == 1 {
		origin, readErr := s.readScanRecoveryOrigin(ctx, q, id)
		if readErr != nil {
			return readErr
		}
		if !hmac.Equal(source, origin.ProofHash) || !hmac.Equal(h, origin.AccountScopeHash) {
			return ErrConflict
		}
		h, e = s.scanRecoveryAccountHash(id, h, source)
		if e != nil {
			return e
		}
	} else if version != 0 || source != nil {
		return ErrConflict
	}
	if !hmac.Equal(h, old) {
		return ErrConflict
	}
	return nil
}
func (s *Store) checkProviderAccount(ctx context.Context, q fundsReader, id int64, p adapter.Provider) error {
	scope, e := adapter.TransferAccountScope(p)
	if e != nil {
		return e
	}
	_, strict := p.(adapter.TransferAccountIdentity)
	return s.checkProviderAccountScope(ctx, q, id, scope, strict)
}

// ValidatePaymentAccount is a local check for presenting still-payable material.
// It performs no provider query and does not change the original order state.
func (g ProviderFlow) ValidatePaymentAccount(ctx context.Context, merchantID int64, tradeNo string) error {
	_, _, _, e := g.owned(ctx, merchantID, tradeNo)
	return e
}
