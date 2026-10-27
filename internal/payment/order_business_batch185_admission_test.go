package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"testing"
)

func TestOrderBatch185RevokedFinalRefundSendAdmissionReleasesOnlyUnclaimedReserve(t *testing.T) {
	s, g, p, o := setupOperations(t)
	ctx := context.Background()
	if _, e := g.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	guard := FundsActionGuard(func(context.Context, pgx.Tx, string, string) error { return identity.ErrFundsAuthorization })
	ctx = context.WithValue(ctx, orderBusinessBatch185SendGuardKey{}, guard)
	_, e := g.Refund(ctx, 1, o.TradeNo, "last-admission185", 60)
	if !errors.Is(e, identity.ErrFundsAuthorization) {
		t.Fatal("revoked final batch send admission ignored", e, p.refunds.Load())
	}
	r, e := s.Refund(ctx, 1, "last-admission185")
	b, be := s.Balance(ctx, 1)
	if e != nil || be != nil || r.Status != "failed" || b.Available != 100 || b.Reserved != 0 || p.refunds.Load() != 0 {
		t.Fatal("revoked never-sent intent retained funds or sent", r.Status, b, p.refunds.Load(), e, be)
	}
	if _, e = s.CheckLedger(ctx); e != nil {
		t.Fatal(e)
	}
}
