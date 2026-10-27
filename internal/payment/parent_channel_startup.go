package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

// ParentStartupSnapshots is trusted process initialization. It authenticates
// current self-parent accounts before any stale import certificate is parsed.
// Alias IDs never replace their authorization origin or grant new permissions.
// No source, account or fee history is written by startup.
func (s *Store) ParentStartupSnapshots(ctx context.Context) (map[int64][]byte, error) {
	records, e := s.ParentStartupAccounts(ctx)
	if e != nil {
		return nil, e
	}
	out := map[int64][]byte{}
	for _, record := range records {
		if record.ChannelID == record.SourceID {
			out[record.ChannelID] = record.Snapshot
		}
	}
	return out, nil
}
func (s *Store) ParentStartupAccounts(ctx context.Context) ([]adapter.ParentStartupAccount, error) {
	if s == nil {
		return nil, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = s.parentPaymentSchema(ctx, tx); e != nil {
		return nil, e
	}
	states, e := s.readParentStates(ctx, tx)
	if e != nil {
		return nil, e
	}
	out := []adapter.ParentStartupAccount{}
	for _, state := range states {
		raw, e := s.openParentAccount(state.account)
		if e != nil {
			return nil, e
		}
		out = append(out, adapter.ParentStartupAccount{ChannelID: state.head.id, SourceID: state.head.source, PayType: state.head.kind, Snapshot: raw})
	}
	return out, tx.Commit(ctx)
}
