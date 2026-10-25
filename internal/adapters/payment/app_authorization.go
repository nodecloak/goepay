package payment

import (
	"context"
	"errors"
)

var ErrApplicationAuthorization = errors.New("application authorization exchange failed")

// ApplicationAuthorizationExchange must use the expected merchant application
// identity sealed by the calling authorization flow, never an untrusted callback
// value. Code is specifically app_auth_code, not a user OAuth auth_code.
type ApplicationAuthorizationExchange struct {
	Code              string `json:"-"`
	ExpectedAuthAppID string `json:"-"`
}

// ApplicationAuthorizationResult is a merchant application's authorization,
// not a user OAuthIdentity. The caller must protect the credential separately;
// ordinary JSON responses deliberately cannot serialize it. No refresh or user
// access token is retained. No channel configuration is changed by this API.
type ApplicationAuthorizationResult struct {
	Provider     string `json:"provider"`
	IssuerAppID  string `json:"issuer_app_id"`
	AuthAppID    string `json:"auth_app_id"`
	UserID       string `json:"user_id"`
	AppAuthToken string `json:"-"`
	// Zero means unknown. This is provider-reported token lifetime, not the
	// local result's display TTL or a refresh token's re_expires_in value.
	TokenExpiresInSeconds int64 `json:"token_expires_in_seconds,omitempty"`
}

type AlipayAuthorizationAppType string

const (
	AlipayAuthorizationMobileApp AlipayAuthorizationAppType = "MOBILEAPP"
	AlipayAuthorizationWebApp    AlipayAuthorizationAppType = "WEBAPP"
	AlipayAuthorizationPublicApp AlipayAuthorizationAppType = "PUBLICAPP"
	AlipayAuthorizationTinyApp   AlipayAuthorizationAppType = "TINYAPP"
	AlipayAuthorizationBaseApp   AlipayAuthorizationAppType = "BASEAPP"
)

type AssignedApplicationAuthorizationLaunch struct {
	PCURL     string `json:"pc_url"`
	WalletURL string `json:"wallet_url"`
}

// ApplicationAuthorizationProvider exposes no payment operations. State
// generation, callback allowlisting, one-use code claims, storage and reveal
// authorization belong to the caller and are not activated by this adapter.
type ApplicationAuthorizationProvider interface {
	ID() string
	ApplicationID() string
	ApplicationAuthorizationURL(state, redirect string) (string, error)
	AssignedApplicationAuthorizationURLs(state, redirect string, appTypes []AlipayAuthorizationAppType) (AssignedApplicationAuthorizationLaunch, error)
	ExchangeApplicationAuthorization(context.Context, ApplicationAuthorizationExchange) (ApplicationAuthorizationResult, error)
}
