package payment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestChannelSharingConsoleReadsScopePaginationIntegrityAndRealCapabilities(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	o := f.paid(t, "sharing-console-list", true)
	first, e := f.Store.ListChannelSharingReceivers(ctx, 751, 0, 1, channelSharingActor)
	if e != nil || len(first.Items) != 1 || first.NextCursor <= 0 {
		t.Fatal(first, e)
	}
	second, e := f.Store.ListChannelSharingReceivers(ctx, 751, first.NextCursor, 1, channelSharingActor)
	if e != nil || len(second.Items) != 1 || second.NextCursor != 0 || first.Items[0].ID == second.Items[0].ID {
		t.Fatal(second, e)
	}
	raw, _ := json.Marshal(second)
	if strings.Contains(string(raw), "second-receiver-5678") {
		t.Fatal("list exposed receiver identity")
	}
	if _, e = f.Store.ListChannelSharingReceivers(ctx, 752, 0, 10, channelSharingActor); !errors.Is(e, ErrInvalid) {
		t.Fatal("receiver list crossed merchant boundary", e)
	}
	f.Flow.Providers[8] = nil
	channels, e := f.Flow.ChannelSharingChannels(ctx, 751, channelSharingActor)
	if e != nil || len(channels) != 1 || channels[0].ChannelID != 7 || len(channels[0].Methods) != 1 || channels[0].Methods[0] != "native" {
		t.Fatal("channel list invented unsupported sharing", channels, e)
	}
	if _, e = f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "sharing-list-order", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	orders, e := f.Store.ListChannelSharingOrders(ctx, 751, 0, 10, channelSharingActor)
	if e != nil || len(orders.Items) != 1 || orders.Items[0].ExternalNo != "sharing-list-order" {
		t.Fatal(orders, e)
	}
	for _, limit := range []int{0, 101, -1} {
		if _, e = f.Store.ListChannelSharingOrders(ctx, 751, 0, limit, channelSharingActor); !errors.Is(e, ErrInvalid) {
			t.Fatal("unbounded order list", limit, e)
		}
	}
	if _, e = f.Store.pool.Exec(ctx, "UPDATE channel_sharing_receivers SET receiver_cipher=decode(repeat('00',32),'hex') WHERE id=$1", first.Items[0].ID); e == nil {
		t.Fatal("immutable encrypted recipient unexpectedly changed")
	}
}
