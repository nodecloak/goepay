package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// ConsoleChannelKinds includes authenticated managed parent IDs in metadata
// controls such as stop/resume. It never synthesizes an executable SDK, loads
// credentials into the shared registry or grants a merchant a payment channel.
func (g ProviderFlow) ConsoleChannelKinds(ctx context.Context) (map[int64]string, error) {
	kinds := map[int64]string{}
	for id, provider := range g.Providers {
		if id <= 0 || provider == nil {
			return nil, ErrInvalid
		}
		kinds[id] = provider.ID()
	}
	if len(kinds) > 1000 {
		return nil, ErrInvalid
	}
	if len(g.ParentFactories) == 0 {
		return kinds, nil
	}
	if g.Store == nil {
		return nil, ErrInvalid
	}
	tx, err := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	ready, err := g.Store.parentPaymentSchema(ctx, tx)
	if err != nil || !ready {
		return kinds, err
	}
	states, err := g.Store.readParentStates(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, state := range states {
		h := state.head
		origin := g.ParentFactories[h.source]
		if origin == nil {
			continue
		}
		if origin.OriginID() != h.source || origin.PayType() != h.kind {
			return nil, ErrConflict
		}
		if existing, ok := kinds[h.id]; ok && existing != h.kind {
			return nil, ErrConflict
		}
		kinds[h.id] = h.kind
		if len(kinds) > 1000 {
			return nil, ErrInvalid
		}
	}
	return kinds, tx.Commit(ctx)
}
