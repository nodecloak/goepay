package payment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestScanCancellation78AlipayDefaultsOffAndInvalidOriginalRequestNeverPerformsIO(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); http.Error(w, "must not call", 500) }))
	defer up.Close()
	key := testKey(t)
	cfg := AlipayConfig{AppID: "local78", SellerID: "seller78", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"scan"}, GatewayURL: up.URL, HTTPClient: up.Client()}
	p, e := NewAlipay(cfg)
	if e != nil {
		t.Fatal(e)
	}
	good := ScanCancellationRequest{TradeNo: "SC78_LOCAL", Amount: 123}
	if p.CanCancelScan() {
		t.Fatal("cancellation enabled by scan payment alone")
	}
	if _, e = p.CancelScan(context.Background(), good); e != ErrUnsupported {
		t.Fatal("default-off cancellation attempted", e)
	}
	cfg.EnableScanCancellation = true
	p, e = NewAlipay(cfg)
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range []ScanCancellationRequest{{TradeNo: "SC78_LOCAL", Amount: 0}, {TradeNo: "../other78", Amount: 123}, {TradeNo: "SC78_LOCAL", ProviderTradeNo: "bad\r\n78", Amount: 123}} {
		if _, e = p.CancelScan(context.Background(), r); e != ErrInvalid {
			t.Fatal("invalid original cancellation request accepted", e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = p.CancelScan(ctx, good); e != ErrInvalid {
		t.Fatal("cancelled context performed work", e)
	}
	if calls.Load() != 0 {
		t.Fatal("invalid or disabled cancellation performed IO", calls.Load())
	}
}
