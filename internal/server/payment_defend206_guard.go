package server

import (
	"context"

	"github.com/jackc/pgx/v5"
	"goepay/internal/payment"
	"goepay/internal/paymentdefend206"
)

// This temporary admission blocks every enabled mode until the original
// verification flows have their own protected, purpose-bound proof.
func (s *Server) paymentDefendContext206(ctx context.Context) context.Context {
	guard := func(ctx context.Context, tx pgx.Tx) error {
		if s.PaymentDefend206 == nil {
			return paymentdefend206.ConfirmUnconfiguredTx(ctx, tx)
		}
		policy, err := s.PaymentDefend206.CurrentTx(ctx, tx)
		if err != nil {
			return err
		}
		if policy.Settings["pay_verify"] != "0" {
			return payment.ErrState
		}
		return nil
	}
	return payment.WithPaymentDefend206Guard(ctx, guard)
}
