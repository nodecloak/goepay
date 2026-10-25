package main

import (
	"crypto/sha256"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/payment"
	"goepay/internal/server"
	"net/url"
	"os"
	"strings"
)

var errConfiguredProviders = errors.New("invalid explicitly configured payment providers")

// configureProviders never discovers a legacy credential file. The configured
// mode requires an explicit encrypted path, origin, and merchant allowlist.
func configureProviders(app *server.Server, mode string, master []byte, origin string) error {
	if app != nil {
		if app.ProviderFlow != nil {
			app.ProviderFlow.ScanRecoveryEnabled = false
		}
		app.IdentityProfiles = nil
		app.IdentityProfileEnabled = false
		app.IdentityProfileCallbackBaseURL = ""
		app.ApplicationAuthorizationProfiles = nil
		app.ApplicationAuthorizationEnabled = false
		app.ApplicationAuthorizationCallbackBaseURL = ""
		app.AuthorizationObservationEnabled = false
	}
	if mode != "configured" {
		return configureSimulation(app, mode, master, origin)
	}
	if app == nil || app.Sandbox || len(master) != 32 {
		return errConfiguredProviders
	}
	path := os.Getenv("GOEPAY_CHANNEL_CONFIG_FILE")
	if path == "" {
		return errConfiguredProviders
	}
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(origin, "#") || u.Path != "" && u.Path != "/" {
		return errConfiguredProviders
	}
	allowRaw := os.Getenv("GOEPAY_ENABLE_PROVIDER_WRITES")
	if allowRaw != "" && allowRaw != "true" && allowRaw != "false" {
		return errConfiguredProviders
	}
	allowWrites := allowRaw == "true"
	recoveryRaw := os.Getenv("GOEPAY_ENABLE_SCAN_RECOVERY")
	if recoveryRaw != "" && recoveryRaw != "true" && recoveryRaw != "false" {
		return errConfiguredProviders
	}
	scanRecovery := recoveryRaw == "true"
	if scanRecovery && (!allowWrites || os.Getenv("GOEPAY_ENABLE_PROVIDER_QUERIES") != "true") {
		return errConfiguredProviders
	}
	imp, e := channelconfig.Load(path, master)
	if e != nil {
		return errConfiguredProviders
	}
	providers, e := channelconfig.BuildWithOptions(imp, channelconfig.BuildOptions{AllowLive: allowWrites, ScanRecovery: scanRecovery})
	if e != nil || len(providers) == 0 {
		return errConfiguredProviders
	}
	oauthEnabled := os.Getenv("GOEPAY_ENABLE_OAUTH")
	if oauthEnabled != "" && oauthEnabled != "false" && oauthEnabled != "true" {
		return errConfiguredProviders
	}
	identityEnabled := os.Getenv("GOEPAY_ENABLE_IDENTITY_LOOKUP")
	if identityEnabled != "" && identityEnabled != "false" && identityEnabled != "true" || identityEnabled == "true" && oauthEnabled != "true" {
		return errConfiguredProviders
	}
	applicationAuthorizationEnabled := os.Getenv("GOEPAY_ENABLE_APPLICATION_AUTHORIZATION")
	if applicationAuthorizationEnabled != "" && applicationAuthorizationEnabled != "false" && applicationAuthorizationEnabled != "true" || applicationAuthorizationEnabled == "true" && oauthEnabled != "true" {
		return errConfiguredProviders
	}
	observationEnabled := os.Getenv("GOEPAY_ENABLE_AUTHORIZATION_OBSERVATION")
	if observationEnabled != "" && observationEnabled != "false" && observationEnabled != "true" {
		return errConfiguredProviders
	}
	applicationAuthorizationProfiles, e := channelconfig.BuildApplicationAuthorizationProfiles(imp, applicationAuthorizationEnabled == "true")
	if e != nil {
		return errConfiguredProviders
	}
	app.IdentityProfileEnabled = identityEnabled == "true"
	app.IdentityProfileCallbackBaseURL = strings.TrimRight(origin, "/")
	app.IdentityProfiles, e = channelconfig.BuildIdentityProfiles(imp, app.IdentityProfileEnabled)
	if e != nil {
		return errConfiguredProviders
	}
	if oauthEnabled == "true" {
		app.OAuthProviders, e = channelconfig.BuildOAuth(imp)
		if e != nil {
			return errConfiguredProviders
		}
		app.MiniOAuthProviders, e = channelconfig.BuildMiniOAuth(imp)
		if e != nil || len(app.OAuthProviders)+len(app.MiniOAuthProviders)+len(app.IdentityProfiles)+len(applicationAuthorizationProfiles) == 0 {
			return errConfiguredProviders
		}
	} else {
		app.OAuthProviders = nil
		app.MiniOAuthProviders = nil
	}
	mapping, e := configuredAllowlist(os.Getenv("GOEPAY_MERCHANT_CHANNELS"), providers)
	if e != nil {
		return e
	}
	key := sha256.Sum256(append([]byte("checkout-signing:"), master...))
	app.CheckoutKey = key[:]
	flow := &payment.ProviderFlow{Store: app.Store, Providers: providers, MerchantChannels: mapping, AllowLive: allowWrites, ScanRecoveryEnabled: scanRecovery, LocalSimulation: false, CallbackBaseURL: strings.TrimRight(origin, "/")}
	app.ProviderFlow = flow
	flow.ReturnURLBuilder = app.ProviderReturnURL
	app.MerchantChannels = mapping
	app.Simulation = nil
	app.AuthorizationObservationEnabled = observationEnabled == "true"
	// Publish this independent capability only after the full configured
	// environment validates. Failure and simulation paths keep it cleared.
	if applicationAuthorizationEnabled == "true" {
		app.ApplicationAuthorizationProfiles = applicationAuthorizationProfiles
		app.ApplicationAuthorizationEnabled = true
		app.ApplicationAuthorizationCallbackBaseURL = strings.TrimRight(origin, "/")
	}
	return nil
}
func configuredAllowlist(raw string, providers map[int64]adapter.Provider) (map[int64][]int64, error) {
	result, e := channelconfig.MerchantAllowlist(raw, providers)
	if e != nil {
		return nil, errConfiguredProviders
	}
	return result, nil
}
