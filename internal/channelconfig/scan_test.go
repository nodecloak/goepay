package channelconfig

import (
	"context"
	"errors"
	provider "goepay/internal/adapters/payment"
	"slices"
	"testing"
)

func TestScanCapabilityRequiresExplicitConfiguration(t *testing.T) {
	c := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "app", "appmchid": "mch", "appkey": "0123456789abcdef0123456789abcdef"}}
	for _, flag := range []string{"", "false", "true"} {
		c.Config["enable_scan_pay"] = flag
		providers, e := Build(Import{Version: 1, Channels: []Channel{c}}, false)
		if e != nil {
			t.Fatal(e)
		}
		if slices.Contains(providers[7].Capabilities().Methods, "scan") != (flag == "true") {
			t.Fatal("incorrect scan capability", flag, providers[7].Capabilities())
		}
		if _, e = providers[7].(provider.ScanProvider).Scan(context.Background(), provider.ScanRequest{}); !errors.Is(e, ErrLiveDisabled) {
			t.Fatal("factory scan write gate bypassed", e)
		}
	}
	c.Config["enable_scan_pay"] = "1"
	if _, e := Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
		t.Fatal("ambiguous scan opt-in accepted")
	}
	ali := certificateChannel(t)
	ali.Config["enable_scan_pay"] = "true"
	providers, e := Build(Import{Version: 1, Channels: []Channel{ali}}, false)
	if e != nil || !slices.Contains(providers[9].Capabilities().Methods, "scan") {
		t.Fatal("certificate scan capability missing", e)
	}
}
