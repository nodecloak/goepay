package channelconfig

import (
	"context"
	provider "goepay/internal/adapters/payment"
)

func (g guarded) ValidateScan(r provider.ScanRequest) error {
	if !g.allowLive {
		return ErrLiveDisabled
	}
	p, ok := g.Provider.(provider.ScanValidator)
	if !ok {
		return provider.ErrUnsupported
	}
	return p.ValidateScan(r)
}
func (g guarded) Scan(ctx context.Context, r provider.ScanRequest) (provider.PaymentEvent, error) {
	if !g.allowLive {
		return provider.PaymentEvent{}, ErrLiveDisabled
	}
	p, ok := g.Provider.(provider.ScanProvider)
	if !ok {
		return provider.PaymentEvent{}, provider.ErrUnsupported
	}
	return p.Scan(ctx, r)
}

func (g guarded) ObserveScan(ctx context.Context, r provider.QueryRequest) (provider.QueryResult, error) {
	p, ok := g.Provider.(provider.ScanObservationProvider)
	if !ok {
		return provider.QueryResult{}, provider.ErrUnsupported
	}
	return p.ObserveScan(ctx, r)
}

func (g guarded) CanCancelScan() bool {
	p, ok := g.Provider.(provider.ScanCancellationProvider)
	return g.allowLive && ok && p.CanCancelScan()
}

func (g guarded) ValidateScanCancellation(r provider.ScanCancellationRequest) error {
	if !g.allowLive {
		return ErrLiveDisabled
	}
	p, ok := g.Provider.(provider.ScanCancellationProvider)
	if !ok {
		return provider.ErrUnsupported
	}
	return p.ValidateScanCancellation(r)
}

func (g guarded) CancelScan(ctx context.Context, r provider.ScanCancellationRequest) (provider.ScanCancellationResult, error) {
	if !g.allowLive {
		return provider.ScanCancellationResult{}, ErrLiveDisabled
	}
	p, ok := g.Provider.(provider.ScanCancellationProvider)
	if !ok {
		return provider.ScanCancellationResult{}, provider.ErrUnsupported
	}
	return p.CancelScan(ctx, r)
}
