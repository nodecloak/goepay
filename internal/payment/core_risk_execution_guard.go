package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type coreRiskExecutionGuardKey struct{}

// WithCoreRiskExecutionGuard is for trusted in-process executors. The guard
// must authenticate its original persisted lease/policy within the supplied
// transaction. It is checked both before work and after every last write.
func WithCoreRiskExecutionGuard(ctx context.Context, guard func(context.Context, pgx.Tx) error) context.Context {
	return context.WithValue(ctx, coreRiskExecutionGuardKey{}, guard)
}
func checkCoreRiskExecutionGuard(ctx context.Context, tx pgx.Tx) error {
	if guard, ok := ctx.Value(coreRiskExecutionGuardKey{}).(func(context.Context, pgx.Tx) error); ok {
		return guard(ctx, tx)
	}
	return nil
}
