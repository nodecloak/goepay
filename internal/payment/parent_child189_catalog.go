package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

func (g ProviderFlow) withParentChildFactory(a parentChannelAccount) (ProviderFlow, error) {
	if a.id == a.source {
		return g, nil
	}
	origin := g.SubchannelFactories[a.source]
	if origin == nil {
		return g, nil
	}
	derive, ok := origin.(interface {
		DeriveParentSubchannel(int64, []byte) (adapter.SubchannelFactory, error)
	})
	if !ok {
		return g, nil
	}
	raw, err := g.Store.openParentAccount(a)
	if err != nil {
		return g, err
	}
	factory, err := derive.DeriveParentSubchannel(a.id, raw)
	if err != nil || factory == nil || factory.Schema().ParentID != a.id || factory.Schema().PayType != a.kind {
		return g, ErrConflict
	}
	out := g
	out.SubchannelFactories = make(map[int64]adapter.SubchannelFactory, len(g.SubchannelFactories)+1)
	for id, f := range g.SubchannelFactories {
		out.SubchannelFactories[id] = f
	}
	out.SubchannelFactories[a.id] = factory
	return out, nil
}

// A super administrator may inspect closed account input schemas for all
// authenticated aliases. This does not grant a merchant a channel or SDK.
func (g ProviderFlow) ParentSubchannelCatalog(ctx context.Context) (ProviderFlow, error) {
	if g.Store == nil || len(g.ParentFactories) == 0 || len(g.SubchannelFactories) == 0 {
		return g, nil
	}
	tx, err := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return g, err
	}
	defer tx.Rollback(ctx)
	ready, err := g.Store.parentPaymentSchema(ctx, tx)
	if err != nil || !ready {
		return g, err
	}
	states, err := g.Store.readParentStates(ctx, tx)
	if err != nil {
		return g, err
	}
	out := g
	for _, state := range states {
		out, err = out.withParentChildFactory(state.account)
		if err != nil {
			return g, err
		}
	}
	return out, tx.Commit(ctx)
}
