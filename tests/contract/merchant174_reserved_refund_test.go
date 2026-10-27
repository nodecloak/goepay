package contract_test

import (
	"context"
	"testing"

	"goepay/internal/compat"
)

func TestMerchant174ClosedAPIKeepsTerminalInternalRefundReplayReadOnly(t *testing.T) {
	f, p, o := merchant174Runtime(t)
	ctx := context.Background()
	r, e := f.s.Store.ReserveRefund(ctx, 2, o.OutTradeNo, "terminal-internal-refund174", 100)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	merchant174Set(t, f, 1, 0, 0)
	out := merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": r.RefundNo, "money": "1.00"})
	if out["code"] != float64(0) || p.refundWrites.Load() != 0 {
		t.Fatal("closed API lost original terminal refund truth or dispatched it", out, p.refundWrites.Load())
	}
}

func TestMerchant174ClosedAPICannotTurnUnsentInternalRefundReservationIntoProviderSend(t *testing.T) {
	f, p, o := merchant174Runtime(t)
	ctx := context.Background()
	reserved, e := f.s.Store.ReserveRefund(ctx, 2, o.OutTradeNo, "internal-unsent-refund174", 100)
	if e != nil {
		t.Fatal(e)
	}
	merchant174Set(t, f, 1, 0, 0)
	out := merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": reserved.RefundNo, "money": "1.00"})
	var operations int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM provider_operations WHERE kind='refund' AND refund_id=$1`, reserved.ID).Scan(&operations); e != nil || operations != 0 || p.refundWrites.Load() != 0 || out["code"] != float64(-1) {
		t.Fatal("closed API dispatched a merely reserved internal refund", operations, p.refundWrites.Load(), out, e)
	}
	// The reservation and its original trusted internal authority remain intact.
	internal, e := f.s.ProviderFlow.Refund(ctx, 2, o.TradeNo, reserved.RefundNo, 100)
	if e != nil || internal.ID != reserved.ID || p.refundWrites.Load() != 1 {
		t.Fatal("API preference orphaned the original internal reservation", internal, e)
	}
}
