package channelconfig

import (
	"context"
	"errors"
	provider "goepay/internal/adapters/payment"
	"reflect"
	"testing"
)

func TestAlipayOrderCodeFactoryKeepsNativeAndWriteGate(t *testing.T) {
	c := certificateChannel(t)
	for _, types := range [][]string{{"8"}, {"8", "3"}, {"3", "8"}, {"3"}} {
		t.Run(types[0]+"-"+types[len(types)-1], func(t *testing.T) {
			c.AppTypes = types
			clients, err := Build(Import{Version: 1, Channels: []Channel{c}}, false)
			if err != nil {
				t.Fatalf("original order-code app type rejected: %v", err)
			}
			p := clients[c.ID]
			// The adapter deliberately has no runtime product-switching API.
			// Inspect its immutable config to verify both mixed ordering cases.
			config := reflect.ValueOf(p.(guarded).Provider).Elem().FieldByName("config")
			wantOffline := len(types) == 1 && types[0] == "8"
			if config.FieldByName("NativeOrderCode").Bool() != wantOffline {
				t.Fatal("ordinary3 must win over8 independent of order", types)
			}
			got := p.Capabilities()
			if len(got.Methods) != 1 || got.Methods[0] != "native" || !got.Query || !got.Refund || !got.RefundQuery {
				t.Fatalf("order-code changed public method/lifecycle: %+v", got)
			}
			if _, err = p.Create(context.Background(), provider.CreateRequest{}); !errors.Is(err, ErrLiveDisabled) {
				t.Fatal("order-code bypassed write gate", err)
			}
		})
	}
}
