package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// FundsActionGuard authorizes a first business intent inside its transaction.
// A request-local guard never changes signed merchant API or worker behavior.
type FundsActionGuard func(context.Context, pgx.Tx, string, string) error
type fundsActionGuardKey struct{}

func WithFundsActionGuard(ctx context.Context, guard FundsActionGuard) context.Context {
	return context.WithValue(ctx, fundsActionGuardKey{}, guard)
}

func requireFundsAction(ctx context.Context, tx pgx.Tx, kind, key string) error {
	guard, _ := ctx.Value(fundsActionGuardKey{}).(FundsActionGuard)
	if guard == nil {
		return nil
	}
	return guard(ctx, tx, kind, key)
}
