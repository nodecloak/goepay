package server

import (
	"context"
	"goepay/internal/payment"
	"testing"
)

func TestRisk177MissingRequiredMarkerCannotBecomeUnconfiguredLegacy(t *testing.T) {
	f, _, creates := risk177Flow(t)
	ctx := context.Background()
	if _, e := f.s.Pool.Exec(ctx, `ALTER TABLE provider_attempts DROP COLUMN core_risk_required CASCADE`); e != nil {
		t.Fatal(e)
	}
	o, e := f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{Version: "v1", MerchantID: 1, OutTradeNo: "risk177-missing-marker", Name: "synthetic marker source", Type: "alipay", Amount: 100, NotifyURL: "https://merchant.example.invalid/notify", ReturnURL: "https://merchant.example.invalid/return"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.177", ""); e == nil || creates.Load() != 0 {
		t.Fatal("current partial schema was treated as unconfigured internal legacy and sent", creates.Load(), e)
	}
}
