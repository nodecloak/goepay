package payment

import (
	"context"
	"testing"
)

func TestVerifiedReceiptAtomicIdentityAndClientIP(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 63001, Name: "receipt fixture", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	orders := []Order{}
	for _, no := range []string{"first", "second"} {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 63001, OutTradeNo: no, Name: no, Type: "wxpay", Amount: 100})
		if e != nil {
			t.Fatal(e)
		}
		if _, owner, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); e != nil || !owner {
			t.Fatal(owner, e)
		}
		orders = append(orders, o)
	}
	if e := s.SetOrderClientIP(ctx, orders[0].ID, "2001:db8::1"); e != nil {
		t.Fatal(e)
	}
	if e := s.SetOrderClientIP(ctx, orders[0].ID, "192.0.2.2"); e != nil {
		t.Fatal(e)
	}
	if e := s.SetOrderClientIP(ctx, orders[0].ID, "not-an-ip"); e == nil {
		t.Fatal("invalid IP accepted")
	}
	r := PaidReceipt{ChannelID: 7, ProviderTradeNo: "verified-upstream-number", BuyerID: "trusted-buyer"}
	for i := 0; i < 2; i++ {
		if e := s.markPaid(ctx, orders[0].TradeNo, "event", true, r); e != nil {
			t.Fatal(e)
		}
	}
	var upstream, ip string
	if e := s.pool.QueryRow(ctx, "SELECT p.provider_trade_no,r.client_ip FROM verified_payment_receipts p JOIN order_reporting r ON r.order_id=p.order_id WHERE p.order_id=$1", orders[0].ID).Scan(&upstream, &ip); e != nil || upstream != r.ProviderTradeNo || ip != "2001:db8::1" {
		t.Fatal(upstream, ip, e)
	}
	conflict := r
	conflict.ProviderTradeNo = "changed"
	if e := s.markPaid(ctx, orders[0].TradeNo, "different-event", true, conflict); e == nil {
		t.Fatal("verified receipt identity changed")
	}
	if e := s.markPaid(ctx, orders[1].TradeNo, "second-event", true, r); e == nil {
		t.Fatal("one upstream receipt paid two orders")
	}
	b, e := s.Balance(ctx, 63001)
	if e != nil || b.Available != 100 {
		t.Fatal(b, e)
	}
	var state string
	if e = s.pool.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1", orders[1].ID).Scan(&state); e != nil || state == "paid" {
		t.Fatal(state, e)
	}
}
