package payment

import (
	"context"
	"crypto/hmac"
	"github.com/jackc/pgx/v5"
)

type OriginalSubchannelContext struct {
	ID             int64 `json:"id,string"`
	ParentID       int64 `json:"parent_id,string"`
	AccountVersion int64 `json:"account_version,string"`
}

// Only typed authenticated original evidence enters this read projection.
func (s *Store) OriginalSubchannelContext(ctx context.Context, mid int64, no string, parent int64) (*OriginalSubchannelContext, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = s.authenticateOriginalOrderBirth(ctx, tx, mid, no); e != nil {
		return nil, e
	}
	schema, e := s.childSchemaPresent(ctx, tx, mid, no)
	if e != nil {
		return nil, e
	}
	if !schema {
		return nil, tx.Commit(ctx)
	}
	b, e := s.readChildRoute(ctx, tx, mid, no)
	if e != nil {
		return nil, e
	}
	quote, found, e := s.readRouteFeeQuote(ctx, tx, mid, no)
	if e != nil {
		return nil, e
	}
	if b == nil {
		if found && quote.facts.Child != nil {
			return nil, ErrConflict
		}
		return nil, tx.Commit(ctx)
	}
	if b.parent != parent || !found || !childCommitmentEqual(quote.facts.Child, b.commitment()) {
		return nil, ErrConflict
	}
	a, e := s.childAccount(ctx, tx, b.child, b.version)
	if e != nil || a.Merchant != mid || a.Parent != parent || !hmac.Equal(a.Proof, b.accountProof) {
		return nil, ErrConflict
	}
	return &OriginalSubchannelContext{b.child, b.parent, b.version}, tx.Commit(ctx)
}
