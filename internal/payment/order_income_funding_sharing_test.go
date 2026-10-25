package payment

import (
	"context"
	"errors"
	"testing"
)

func TestIncomeFundingSharingReturnRestoresOnlyAvailableAndNeverAnotherHold(t *testing.T) {
	f := newSharingReturnFixture(t)
	ctx := context.Background()
	o, err := scanOrder(f.Store.pool.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", f.Sharing.OrderID))
	if err != nil {
		t.Fatal(err)
	}
	h, err := f.Store.FreezeOrderIncome(ctx, 751, o.TradeNo, "original-ps-hold", incomeTestActor)
	if err != nil || h.Amount != 80 || h.ChannelSharingAmount != 20 {
		t.Fatal(h, err)
	}
	other := incomePaidOrder(t, f.Store, 751, "another-held-source")
	otherHold, err := f.Store.FreezeOrderIncome(ctx, 751, other.TradeNo, "other-hold", incomeTestActor)
	if err != nil || otherHold.Amount != 100 {
		t.Fatal(otherHold, err)
	}
	p, err := f.Store.RefundFundingPreview(ctx, f.Store.pool, o.ID, "full-original", 100)
	if err != nil || p.HeldAmount != 80 || p.AvailableAmount != 20 {
		t.Fatal(p, err)
	}
	if _, err = f.Store.ReserveRefund(ctx, 751, o.OutTradeNo, "full-original", 100); !errors.Is(err, ErrInsufficient) {
		t.Fatal("another original hold funded the missing PS outflow", err)
	}
	var count int
	if err = f.Store.pool.QueryRow(ctx, "SELECT count(*) FROM refunds WHERE order_id=$1", o.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed reserve persisted source or request", count, err)
	}
	f.Phase.Store(1)
	ret, err := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, f.Sharing.Items[0].ReceiverID, "return-original-twenty", 20, channelSharingActor)
	if err != nil || ret.State != "succeeded" || f.Sends.Load() != 1 {
		t.Fatal(ret, err, f.Sends.Load())
	}
	h, err = f.Store.OrderIncomeHold(ctx, 751, o.TradeNo)
	if err != nil || h.Amount != 80 || h.RemainingAmount != 80 || h.ChannelSharingAmount != 20 {
		t.Fatal("credible PR enlarged the immutable original hold", h, err)
	}
	p, err = f.Store.RefundFundingPreview(ctx, f.Store.pool, o.ID, "full-original", 100)
	if err != nil || p.HeldAmount != 80 || p.AvailableAmount != 20 {
		t.Fatal("PR was attributed to held principal rather than available", p, err)
	}
	r, err := f.Store.ReserveRefund(ctx, 751, o.OutTradeNo, "full-original", 100)
	if err != nil {
		t.Fatal(err)
	}
	if incomeScopeBalance(t, f.Store, 751, "order_income_reserve", otherHold.ID) != 100 || incomeScopeBalance(t, f.Store, 751, "order_income_refund_reserve", r.ID) != 100 {
		t.Fatal("refund borrowed or changed another original source")
	}
	if err = f.Store.ResolveRefund(ctx, r.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if released, err := f.Store.UnfreezeOrderIncome(ctx, 751, o.TradeNo, h.ID, incomeTestActor); err != nil || released.State != "released" {
		t.Fatal(released, err)
	}
	if incomeScopeBalance(t, f.Store, 751, "order_income_reserve", otherHold.ID) != 100 {
		t.Fatal("zero release consumed the independent hold")
	}
	report, err := f.Store.CheckLedger(ctx)
	if err != nil || report.InvalidOrderIncomeFundings != 0 || report.InvalidRefundFundings != 0 || report.InvalidChannelSharingReturns != 0 || report.InvalidProviderRefundResults != 0 {
		t.Fatal(report, err)
	}
}
