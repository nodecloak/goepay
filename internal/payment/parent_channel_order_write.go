package payment

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// The account used by an existing order remains immutable. A new outgoing
// action still needs the parent's current write authorization. Keep these two
// checks separate; rotating credentials must not change the original account.
func (g ProviderFlow) checkParentOrderWriteTx(ctx context.Context, tx pgx.Tx, mid, orderID, channel int64) error {
	resolved, err := g.parentPaymentOrder(ctx, tx, mid, orderID)
	if err != nil {
		return err
	}
	if !resolved.paymentWritesEnabled() {
		return ErrLiveDisabled
	}
	b := resolved.parentBindings[channel]
	if b == nil {
		return nil
	}
	if err = parentChannelLock(ctx, tx, channel); err != nil {
		return err
	}
	return resolved.Store.checkParentRuntimeWriteTx(ctx, tx, b, mid, channel)
}

func (g ProviderFlow) checkParentOrderWrite(ctx context.Context, mid, orderID, channel int64) error {
	tx, err := g.Store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = g.checkParentOrderWriteTx(ctx, tx, mid, orderID, channel); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (g ProviderFlow) parentOrderWriteGuard(mid, orderID, channel int64) func(context.Context, pgx.Tx) error {
	return func(ctx context.Context, tx pgx.Tx) error {
		return g.checkParentOrderWriteTx(ctx, tx, mid, orderID, channel)
	}
}
func (g ProviderFlow) parentLiveWriteGuard(context.Context, pgx.Tx) error {
	if !g.paymentWritesEnabled() {
		return ErrLiveDisabled
	}
	return nil
}
