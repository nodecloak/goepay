package channelconfig

import (
	"context"
	provider "goepay/internal/adapters/payment"
	"net/http"
)

func (g guarded) Freeze(ctx context.Context, r provider.CreateRequest) (provider.CreateResult, error) {
	if !g.allowLive {
		return provider.CreateResult{}, ErrLiveDisabled
	}
	p, ok := g.Provider.(provider.PreauthorizationProvider)
	if !ok {
		return provider.CreateResult{}, provider.ErrUnsupported
	}
	return p.Freeze(ctx, r)
}
func (g guarded) VerifyAuthorization(ctx context.Context, h http.Header, raw []byte, e provider.ExpectedPayment) (provider.AuthorizationEvent, error) {
	p, ok := g.Provider.(provider.PreauthorizationProvider)
	if !ok {
		return provider.AuthorizationEvent{}, provider.ErrUnsupported
	}
	return p.VerifyAuthorization(ctx, h, raw, e)
}
func (g guarded) CaptureAuthorization(ctx context.Context, r provider.AuthorizationCaptureRequest) (provider.PaymentEvent, error) {
	if !g.allowLive {
		return provider.PaymentEvent{}, ErrLiveDisabled
	}
	p, ok := g.Provider.(provider.PreauthorizationProvider)
	if !ok {
		return provider.PaymentEvent{}, provider.ErrUnsupported
	}
	return p.CaptureAuthorization(ctx, r)
}
func (g guarded) QueryAuthorization(ctx context.Context, r provider.AuthorizationRequest) (provider.AuthorizationEvent, error) {
	p, ok := g.Provider.(provider.PreauthorizationProvider)
	if !ok {
		return provider.AuthorizationEvent{}, provider.ErrUnsupported
	}
	return p.QueryAuthorization(ctx, r)
}
func (g guarded) UnfreezeAuthorization(ctx context.Context, r provider.AuthorizationRequest) (provider.AuthorizationEvent, error) {
	if !g.allowLive {
		return provider.AuthorizationEvent{}, ErrLiveDisabled
	}
	p, ok := g.Provider.(provider.PreauthorizationProvider)
	if !ok {
		return provider.AuthorizationEvent{}, provider.ErrUnsupported
	}
	return p.UnfreezeAuthorization(ctx, r)
}
func (g guarded) CancelAuthorization(ctx context.Context, r provider.AuthorizationRequest) (provider.AuthorizationEvent, error) {
	if !g.allowLive {
		return provider.AuthorizationEvent{}, ErrLiveDisabled
	}
	p, ok := g.Provider.(provider.PreauthorizationProvider)
	if !ok {
		return provider.AuthorizationEvent{}, provider.ErrUnsupported
	}
	return p.CancelAuthorization(ctx, r)
}
