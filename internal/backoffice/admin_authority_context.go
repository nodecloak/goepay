package backoffice

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type adminAuthorityContextKey struct{}

// WithAdminAuthorityGuard carries the authenticated HTTP actor into sensitive
// administrator transactions. The guard must retain the actor/session locks
// until commit and recheck the original session deadline on subsequent calls.
// Direct service callers retain the existing explicit Principal boundary.
func WithAdminAuthorityGuard(ctx context.Context, guard func(context.Context, pgx.Tx) error) context.Context {
	return context.WithValue(ctx, adminAuthorityContextKey{}, guard)
}

func requireAdminAuthorityTx(ctx context.Context, tx pgx.Tx) error {
	guard, ok := ctx.Value(adminAuthorityContextKey{}).(func(context.Context, pgx.Tx) error)
	if !ok || guard == nil {
		return nil
	}
	if err := guard(ctx, tx); err != nil {
		return fmt.Errorf("%w: administrator authority: %w", ErrForbidden, err)
	}
	return nil
}
