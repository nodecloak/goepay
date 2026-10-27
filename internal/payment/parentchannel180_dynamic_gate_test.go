package payment

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"testing"
)

func TestParentChannel180EveryPreparedWriteRechecksRootGate(t *testing.T) {
	g := ProviderFlow{AllowLive: true, parentLive: func() bool { return false }}
	ctx := context.Background()
	tests := map[string]func() error{
		"freeze":  func() error { _, e := g.SubmitPreauthorization(ctx, "trade", 8, "", ""); return e },
		"capture": func() error { _, e := g.CapturePreauthorization(ctx, 1, "trade"); return e },
		"release": func() error { _, e := g.ReleaseAuthorization(ctx, 1, "trade"); return e },
		"remaining": func() error {
			_, e := g.ReleaseRemainingAuthorization(ctx, 1, "trade", PreauthRemainingReleaseInput{}, "", "", nil)
			return e
		},
		"scan":     func() error { return g.ValidateScanRequest(8, adapter.ScanRequest{}) },
		"recovery": func() error { _, e := g.ExecuteScanRecovery(ctx, 1, ""); return e },
		"transfer": func() error { _, e := g.Transfer(ctx, 1, 8, "transfer", adapter.TransferRequest{}); return e },
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			if e := call(); !errors.Is(e, ErrLiveDisabled) {
				t.Fatalf("prepared writer bypasses current root: %v", e)
			}
		})
	}
}
