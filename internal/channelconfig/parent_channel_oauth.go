package channelconfig

import (
	"context"
	adapter "goepay/internal/adapters/payment"
)

func parentOAuthAllowed(options ParentBuildOptions, mini bool) func() bool {
	return func() bool {
		if !options.OAuthEnabled || !options.AllowLive || options.RuntimeWriteAllowed != nil && !options.RuntimeWriteAllowed() {
			return false
		}
		allow := options.WebOAuthAllowed
		if mini {
			allow = options.MiniOAuthAllowed
		}
		return allow == nil || allow()
	}
}

type parentWebOAuthGuard struct {
	adapter.WebOAuthProvider
	allowed func() bool
}

func (p parentWebOAuthGuard) OAuthAuthorizationURL(state, redirect string) (string, error) {
	if !p.allowed() {
		return "", ErrLiveDisabled
	}
	return p.WebOAuthProvider.OAuthAuthorizationURL(state, redirect)
}
func (p parentWebOAuthGuard) ExchangeOAuth(ctx context.Context, code string) (adapter.OAuthIdentity, error) {
	if !p.allowed() {
		return adapter.OAuthIdentity{}, ErrLiveDisabled
	}
	return p.WebOAuthProvider.ExchangeOAuth(ctx, code)
}

type parentMiniOAuthGuard struct {
	adapter.MiniOAuthProvider
	allowed func() bool
}

func (p parentMiniOAuthGuard) ExchangeMiniCode(ctx context.Context, code string) (adapter.OAuthIdentity, error) {
	if !p.allowed() {
		return adapter.OAuthIdentity{}, ErrLiveDisabled
	}
	return p.MiniOAuthProvider.ExchangeMiniCode(ctx, code)
}
