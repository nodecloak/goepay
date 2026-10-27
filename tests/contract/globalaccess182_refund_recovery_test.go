package contract_test

import (
	"context"
	"errors"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
)

type blockedRefund182 struct {
	*merchant174Provider
	entered chan struct{}
	resume  chan struct{}
}

func (p *blockedRefund182) Refund(ctx context.Context, r adapter.RefundRequest) (adapter.RefundResult, error) {
	p.refundWrites.Add(1)
	close(p.entered)
	select {
	case <-p.resume:
		return adapter.RefundResult{}, &adapter.UnknownOutcome{Operation: "synthetic refund", Cause: errors.New("local response lost")}
	case <-ctx.Done():
		return adapter.RefundResult{}, &adapter.UnknownOutcome{Operation: "synthetic refund", Cause: ctx.Err()}
	}
}

func TestGlobalAccess182ClaimedWinnerKeepsFundsAcrossClosureAndRecoversOnce(t *testing.T) {
	f, base, o := merchant174Runtime(t)
	p := &blockedRefund182{merchant174Provider: base, entered: make(chan struct{}), resume: make(chan struct{})}
	f.s.ProviderFlow.Providers[7] = p
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.s.ProviderFlow.RefundFromAPI(ctx, 2, o.TradeNo, "claimed182", 100)
		done <- err
	}()
	select {
	case <-p.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer func() {
		select {
		case <-p.resume:
		default:
			close(p.resume)
		}
	}()
	globalSetting179(t, f, "user_refund", "0")
	merchant174Set(t, f, 1, 0, 0)
	_, err := f.s.ProviderFlow.RefundFromAPI(ctx, 2, o.TradeNo, "claimed182", 100)
	var unknown *adapter.UnknownOutcome
	if !errors.As(err, &unknown) {
		t.Fatal("claimed replay lost original uncertainty", err)
	}
	r, err := f.s.Store.Refund(ctx, 2, "claimed182")
	b, be := f.s.Store.Balance(ctx, 2)
	if err != nil || be != nil || r.Status != "pending" || b.Available != 900 || b.Reserved != 100 || p.refundWrites.Load() != 1 {
		t.Fatal("claimed winner was locally released or resent", r.Status, b, p.refundWrites.Load(), err, be)
	}
	close(p.resume)
	if err = <-done; !errors.As(err, &unknown) {
		t.Fatal("synthetic lost response did not remain unknown", err)
	}
	r, err = f.s.ProviderFlow.QueryRefund(ctx, 2, "claimed182")
	b, be = f.s.Store.Balance(ctx, 2)
	if err != nil || be != nil || r.Status != "succeeded" || b.Available != 900 || b.Reserved != 0 || p.refundWrites.Load() != 1 {
		t.Fatal("closed gate lost original query recovery", r.Status, b, p.refundWrites.Load(), err, be)
	}
	if _, err = f.s.Store.CheckLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if out := merchant174Invoke(t, f, "/api/pay/refundquery", compat.Params{"out_refund_no": "claimed182"}); out["code"] != float64(0) {
		t.Fatal("signed original query lost terminal result", out)
	}
}
