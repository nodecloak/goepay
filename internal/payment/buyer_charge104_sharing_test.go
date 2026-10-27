package payment

import (
	"errors"
	"testing"
)

func TestBuyerCharge104SharingFinishRetainsActualGrossAndAuthenticatesBeforeRecovery(t *testing.T) {
	base := newSharingExecutionFixture(t)
	s := base.Store
	ctx := t.Context()
	threshold, cost := int64(1), int64(6)
	buyer104PolicyFixture(t, s, "global", 0, 0, &threshold, &cost)
	buyer104PolicyFixture(t, s, "merchant", 751, 1, nil, nil)
	sharingPaymentRule(t, base.Flow, 2000)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "sharing-actual104", Name: "synthetic actual sharing", Type: "wxpay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = base.Flow.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.markPaid(ctx, o.TradeNo, "synthetic-sharing104", true, PaidReceipt{RealMoney: 106, ChannelID: 7, ProviderTradeNo: "WX_" + o.TradeNo}); e != nil {
		t.Fatal(e)
	}
	finish := sharingFinishFixtureForOrder(t, base, o)
	view, e := finish.Flow.PreviewChannelSharingFinish(ctx, 751, o.TradeNo, channelSharingActor)
	if e != nil || view.GrossAmount != 106 || view.ExpectedUnsplit != 106 || view.MerchantNet != 100 || view.ReleaseAmount != 100 {
		t.Fatal("finish uses M instead of actual C", view, e)
	}
	initial, e := finish.Flow.SubmitChannelSharingFinish(ctx, 751, o.TradeNo, "actual-finish104", channelSharingActor)
	if e != nil || initial.ExpectedUnsplit != 106 {
		t.Fatal(initial, e)
	}
	finish.Phase.Store(1)
	final, e := finish.Flow.QueryChannelSharingFinish(ctx, 751, "actual-finish104", channelSharingActor)
	if e != nil || !final.FundsReleased || final.ReleaseAmount != 100 || final.GrossAmount != 106 {
		t.Fatal(final, e)
	}
	balance, e := s.Balance(ctx, 751)
	if e != nil || balance.Available != 100 || balance.Reserved != 0 {
		t.Fatal(balance, e)
	}
	queries := finish.Queries.Load()
	if _, e = s.pool.Exec(ctx, `ALTER TABLE order_customer_charges104 DISABLE TRIGGER USER;DELETE FROM order_customer_charges104;ALTER TABLE order_customer_charges104 ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = finish.Flow.QueryChannelSharingFinish(ctx, 751, "actual-finish104", channelSharingActor); !errors.Is(e, ErrConflict) || finish.Queries.Load() != queries {
		t.Fatal("lost source reached recovery upstream", e, finish.Queries.Load(), queries)
	}
}
