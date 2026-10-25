package payment

import (
	"context"
	"testing"
)

func TestChannelSharingRechargeRetainsOriginalPurposeWithoutDistributingBalanceTopup(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	sharingPaymentRule(t, f.Flow, 2000)
	o, e := f.Store.CreateRechargeOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "recharge:sharing-purpose", Type: "wxpay", Amount: 100, Name: "balance recharge"}, "local-test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	d, e := f.Store.ChannelSharingPayment(ctx, 751, o.TradeNo, channelSharingActor)
	if e != nil || d.Declared || len(d.Items) != 0 {
		t.Fatal("balance recharge was distributed by ordinary payment rules", d, e)
	}
	if e = f.Store.markPaid(ctx, o.TradeNo, "local-recharge-receipt", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "WX_" + o.TradeNo}); e != nil {
		t.Fatal(e)
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 100 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
	if _, e = f.Store.pool.Exec(ctx, "DELETE FROM recharge_intents WHERE order_id=$1", o.ID); e == nil {
		t.Fatal("recharge purpose could be removed after submission")
	}
	if _, e = f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "invalid-recharge-sharing", channelSharingActor); e == nil {
		t.Fatal("balance recharge accepted a PS operation")
	}
}
