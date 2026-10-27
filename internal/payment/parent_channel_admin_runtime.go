package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

// AdminParentRuntimeChannel returns the authenticated immutable fee identity
// consumed by the existing 96/99 policy engines. It exposes no execution keys.
func (g ProviderFlow) AdminParentRuntimeChannel(ctx context.Context, id int64) (RuntimeChannel, error) {
	if g.Store == nil || id <= 0 {
		return RuntimeChannel{}, ErrInvalid
	}
	tx, e := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return RuntimeChannel{}, e
	}
	defer tx.Rollback(ctx)
	ready, e := g.Store.parentPaymentSchema(ctx, tx)
	if e != nil {
		return RuntimeChannel{}, e
	}
	if !ready {
		return g.RuntimeChannel(id)
	}
	head, e := g.Store.readParentHead(ctx, tx, id)
	if errors.Is(e, ErrNotFound) {
		var born bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-channels' AND resource_id=$1)`, id).Scan(&born); e != nil {
			return RuntimeChannel{}, e
		}
		if born {
			return RuntimeChannel{}, ErrConflict
		}
		return g.RuntimeChannel(id)
	}
	if e != nil {
		return RuntimeChannel{}, e
	}
	if head.archived {
		return RuntimeChannel{}, ErrNotFound
	}
	if _, e = g.Store.readParentAccount(ctx, tx, id, head.version); e != nil {
		return RuntimeChannel{}, e
	}
	return head.fee, tx.Commit(ctx)
}
