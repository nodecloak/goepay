package payment

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// PaymentDefend206Guard is installed only by the authenticated payment
// entry point. It checks the protected policy in the same transaction as the
// next durable business write, without holding a database connection over I/O.
type PaymentDefend206Guard func(context.Context, pgx.Tx) error

type paymentDefend206GuardKey struct{}

func WithPaymentDefend206Guard(ctx context.Context, guard PaymentDefend206Guard) context.Context {
	return context.WithValue(ctx, paymentDefend206GuardKey{}, guard)
}

func CheckPaymentDefend206Tx(ctx context.Context, tx pgx.Tx) error {
	guard, ok := ctx.Value(paymentDefend206GuardKey{}).(PaymentDefend206Guard)
	if !ok {
		return nil
	}
	if guard == nil || tx == nil {
		return ErrState
	}
	return guard(ctx, tx)
}
