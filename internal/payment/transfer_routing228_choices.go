package payment

import (
	"context"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

type TransferRoutingChoice228 struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Types []string `json:"types"`
}

// A global catalogue describes SDK capabilities, not merchant permissions. A
// later execution must still intersect the merchant's signed and live grants.
// Restoration is local; listing or saving settings never sends a provider call.
func (g *ProviderFlow) TransferRoutingChoices228(ctx context.Context) ([]TransferRoutingChoice228, error) {
	tx, err := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	states, err := g.Store.readParentStates(ctx, tx)
	if err != nil {
		return nil, err
	}
	providers := map[int64]adapter.Provider{}
	names := map[int64]string{}
	for id, p := range g.Providers {
		if p != nil {
			providers[id] = p
			names[id] = p.ID()
		}
	}
	for _, state := range states {
		delete(providers, state.head.id)
		if state.head.archived || !state.policy.enabled {
			continue
		}
		execution, err := g.restoreParentExecution(state.account)
		if err != nil {
			return nil, err
		}
		providers[state.head.id] = execution.Payment
		names[state.head.id] = state.head.name
	}
	ids := []int64{}
	for id := range providers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := []TransferRoutingChoice228{}
	for _, id := range ids {
		p := providers[id]
		caps, ok := p.(adapter.TransferCapabilities)
		if !ok {
			continue
		}
		if _, ok = p.(adapter.TransferProvider); !ok {
			continue
		}
		types := []string{}
		for _, kind := range caps.TransferTypes() {
			for _, allowed := range transferRoutingTypes228 {
				if kind == allowed {
					types = append(types, kind)
				}
			}
		}
		if len(types) > 0 {
			out = append(out, TransferRoutingChoice228{ID: strconv.FormatInt(id, 10), Name: names[id], Types: types})
		}
	}
	return out, tx.Commit(ctx)
}
