package server

import (
	"bytes"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/backoffice"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"testing"
)

func TestPayRollStrategyBoundaries(t *testing.T) {
	c := []backoffice.PayRollChannel{{ID: 7, Weight: 2}, {ID: 8, Weight: 1}, {ID: 9, Weight: 0}}
	for _, scenario := range []struct {
		strategy     string
		cursor, draw int64
		want         int
	}{{"round_robin", 0, 0, 0}, {"round_robin", 4, 0, 1}, {"first", 2, 0, 0}, {"weighted", 0, 0, 0}, {"weighted", 0, 1, 0}, {"weighted", 0, 2, 1}, {"priority", 0, 0, 0}} {
		got, e := payRollIndex(scenario.strategy, c, scenario.cursor, scenario.draw)
		if e != nil || got != scenario.want {
			t.Fatal(scenario, got, e)
		}
	}
	for _, draw := range []int64{-1, 3} {
		if _, e := payRollIndex("weighted", c, 0, draw); e == nil {
			t.Fatal("weighted invalid draw accepted", draw)
		}
	}
	c[1].Weight = 2
	if got, e := payRollIndex("priority", c, 0, 1); e != nil || got != 1 {
		t.Fatal("priority tie is not limited to maximum", got, e)
	}
	if _, e := payRollIndex("weighted", []backoffice.PayRollChannel{{ID: 7}}, 0, 0); e == nil {
		t.Fatal("all zero weights selected a provider")
	}
}

func TestPayRollBindingIgnoresTransportFieldsButPinsApplication(t *testing.T) {
	wx, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "trusted", MerchantID: "mch", APIKey: "0123456789abcdef0123456789abcdef"})
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{CheckoutKey: bytes.Repeat([]byte{1}, 32), ProviderFlow: &payment.ProviderFlow{Providers: map[int64]adapter.Provider{7: wx}}}
	p := compat.Params{"type": "wxpay", "out_trade_no": "one", "name": "item"}
	before := s.routeRequestHash(1, p, 7, "native", 100)
	p["timestamp"] = "changed"
	p["sign"] = "changed"
	p["clientip"] = "198.51.100.5"
	if !bytes.Equal(before, s.routeRequestHash(1, p, 7, "native", 100)) {
		t.Fatal("retry timestamp/IP changed immutable binding")
	}
	for _, change := range []struct{ mid, ch, amount int64 }{{2, 7, 100}, {1, 8, 100}, {1, 7, 101}} {
		if bytes.Equal(before, s.routeRequestHash(change.mid, p, change.ch, "native", change.amount)) {
			t.Fatal("different immutable scope accepted", change)
		}
	}
	other, _ := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "other-app", MerchantID: "mch", APIKey: "0123456789abcdef0123456789abcdef"})
	s.ProviderFlow.Providers[7] = other
	if bytes.Equal(before, s.routeRequestHash(1, p, 7, "native", 100)) {
		t.Fatal("trusted AppID replacement reused old route")
	}
}
