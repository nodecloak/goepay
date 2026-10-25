package channelconfig

import (
	"context"
	"errors"
	"testing"

	provider "goepay/internal/adapters/payment"
)

func TestScanRecovery78FactoryDefaultOffAndIndependentWriteGuard(t *testing.T) {
	c := certificateChannel(t)
	c.Config["enable_scan_pay"] = "true"
	c.Config["appmchid"] = "synthetic-seller78"
	imp := Import{Version: 1, Channels: []Channel{c}}
	for _, live := range []bool{false, true} {
		clients, e := Build(imp, live)
		if e != nil {
			t.Fatal(e)
		}
		cancel, ok := clients[c.ID].(provider.ScanCancellationProvider)
		if !ok || cancel.CanCancelScan() {
			t.Fatal("legacy factory did not expose a default-off cancellation gate")
		}
		if _, ok := clients[c.ID].(provider.ScanObservationProvider); !ok {
			t.Fatal("guard lost authenticated read capability")
		}
	}
	for _, live := range []bool{false, true} {
		clients, e := BuildWithOptions(imp, BuildOptions{AllowLive: live, ScanRecovery: true})
		if e != nil {
			t.Fatal(e)
		}
		cancel := clients[c.ID].(provider.ScanCancellationProvider)
		if cancel.CanCancelScan() != live {
			t.Fatal("independent recovery option bypassed ordinary write gate")
		}
		if !live {
			if _, e = cancel.CancelScan(context.Background(), provider.ScanCancellationRequest{}); !errors.Is(e, ErrLiveDisabled) {
				t.Fatal("read-only guard admitted cancellation", e)
			}
			if e = cancel.ValidateScanCancellation(provider.ScanCancellationRequest{}); !errors.Is(e, ErrLiveDisabled) {
				t.Fatal("read-only guard admitted cancellation preflight", e)
			}
		}
	}
	c.Config["appmchid"] = ""
	if _, e := BuildWithOptions(Import{Version: 1, Channels: []Channel{c}}, BuildOptions{AllowLive: true, ScanRecovery: true}); e == nil {
		t.Fatal("recovery admitted missing original seller identity")
	}
	wx := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "app78", "appmchid": "mch78", "appkey": "0123456789abcdef0123456789abcdef", "enable_scan_pay": "true"}}
	if _, e := BuildWithOptions(Import{Version: 1, Channels: []Channel{wx}}, BuildOptions{AllowLive: true, ScanRecovery: true}); e == nil {
		t.Fatal("recovery admitted missing original mutual TLS credentials")
	}
}
