package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"goepay/internal/channelconfig"
	"goepay/internal/server"
	"strings"
	"testing"
)

func configuredApplicationAuthorizationImport(t *testing.T) channelconfig.Import {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "payment-app", "appmchid": "local-mch", "appkey": "0123456789abcdef0123456789abcdef"}}}, ApplicationAuthorizationProfiles: []channelconfig.ApplicationAuthorizationProfileSpec{{ID: 21, Revision: 1, Enabled: true, IssuerAppID: "2021000000000001", ExpectedAuthAppID: "2021000000000002", LaunchKind: "basic", MerchantIDs: channelconfig.IdentityMerchantIDs{1000}, Config: map[string]string{"appsecret": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), "appkey": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)}))}}}}
}

func TestConfiguredApplicationAuthorizationExplicitGateIndependentOfWrites(t *testing.T) {
	master := bytes.Repeat([]byte{77}, 32)
	imp := configuredApplicationAuthorizationImport(t)
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", oauthImportFile(t, master, imp))
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1000":[7]}`)
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "false")
	t.Setenv("GOEPAY_ENABLE_IDENTITY_LOOKUP", "false")
	t.Setenv("GOEPAY_ENABLE_OAUTH", "false")
	t.Setenv("GOEPAY_ENABLE_APPLICATION_AUTHORIZATION", "")
	app := &server.Server{}
	if err := configureProviders(app, "configured", master, "https://pay.example.invalid"); err != nil || app.ApplicationAuthorizationEnabled || len(app.ApplicationAuthorizationProfiles) != 0 || app.ApplicationAuthorizationCallbackBaseURL != "" {
		t.Fatal("application authorization not default off", err)
	}
	t.Setenv("GOEPAY_ENABLE_OAUTH", "true")
	t.Setenv("GOEPAY_ENABLE_APPLICATION_AUTHORIZATION", "true")
	if err := configureProviders(app, "configured", master, "https://pay.example.invalid/"); err != nil || !app.ApplicationAuthorizationEnabled || len(app.ApplicationAuthorizationProfiles) != 1 || app.ApplicationAuthorizationCallbackBaseURL != "https://pay.example.invalid" || app.ProviderFlow.AllowLive || len(app.OAuthProviders) != 0 || len(app.MiniOAuthProviders) != 0 {
		t.Fatal("standalone authorization incorrectly depends on payment writes or user OAuth", err)
	}
	p := app.ApplicationAuthorizationProfiles[21]
	if p.IssuerAppID != "2021000000000001" || p.ExpectedAuthAppID != "2021000000000002" || !p.AllowsMerchant(1000) {
		t.Fatal("configured profile application/allowance changed")
	}
	if _, err := p.Adapter.ApplicationAuthorizationURL(strings.Repeat("a", 64), app.ApplicationAuthorizationCallbackBaseURL+"/cb/"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOEPAY_ENABLE_OAUTH", "false")
	t.Setenv("GOEPAY_ENABLE_APPLICATION_AUTHORIZATION", "false")
	if err := configureProviders(app, "configured", master, "https://pay.example.invalid"); err != nil || app.ApplicationAuthorizationEnabled || len(app.ApplicationAuthorizationProfiles) != 0 || app.ApplicationAuthorizationCallbackBaseURL != "" {
		t.Fatal("disabled application authorization retained runtime capability", err)
	}
}

func TestConfiguredApplicationAuthorizationRejectsInvalidGatesAndClearsState(t *testing.T) {
	master := bytes.Repeat([]byte{78}, 32)
	imp := configuredApplicationAuthorizationImport(t)
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", oauthImportFile(t, master, imp))
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1000":[7]}`)
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "false")
	t.Setenv("GOEPAY_ENABLE_IDENTITY_LOOKUP", "false")
	t.Setenv("GOEPAY_ENABLE_OAUTH", "true")
	t.Setenv("GOEPAY_ENABLE_APPLICATION_AUTHORIZATION", "true")
	seeded := func() *server.Server {
		return &server.Server{ApplicationAuthorizationEnabled: true, ApplicationAuthorizationCallbackBaseURL: "https://stale.example.invalid", ApplicationAuthorizationProfiles: map[int64]channelconfig.ApplicationAuthorizationProfile{99: {ID: 99}}}
	}
	assertCleared := func(app *server.Server) {
		t.Helper()
		if app.ApplicationAuthorizationEnabled || len(app.ApplicationAuthorizationProfiles) != 0 || app.ApplicationAuthorizationCallbackBaseURL != "" {
			t.Fatal("failed configuration retained authorization state")
		}
	}
	for _, origin := range []string{"http://pay.example.invalid", "https://user@pay.example.invalid", "https://pay.example.invalid/path", "https://pay.example.invalid?q=1", "https://pay.example.invalid#frag", "https://pay.example.invalid?", "https://pay.example.invalid#", "https://:443"} {
		app := seeded()
		if err := configureProviders(app, "configured", master, origin); err == nil {
			t.Fatal("untrusted origin accepted")
		}
		assertCleared(app)
	}
	for _, gate := range []string{"true", "yes"} {
		t.Setenv("GOEPAY_ENABLE_APPLICATION_AUTHORIZATION", gate)
		t.Setenv("GOEPAY_ENABLE_OAUTH", "false")
		app := seeded()
		if err := configureProviders(app, "configured", master, "https://pay.example.invalid"); err == nil {
			t.Fatal("invalid OAuth/application gate accepted")
		}
		assertCleared(app)
	}
	t.Setenv("GOEPAY_ENABLE_OAUTH", "true")
	t.Setenv("GOEPAY_ENABLE_APPLICATION_AUTHORIZATION", "true")
	app := seeded()
	app.Sandbox = true
	if err := configureProviders(app, "configured", master, "https://pay.example.invalid"); err == nil {
		t.Fatal("sandbox activated external authorization")
	}
	assertCleared(app)
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1000":[999]}`)
	app = seeded()
	if err := configureProviders(app, "configured", master, "https://pay.example.invalid"); err == nil {
		t.Fatal("invalid payment environment accepted")
	}
	assertCleared(app)
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", "must-not-read-in-simulation")
	app = seeded()
	app.Sandbox = true
	if err := configureProviders(app, "simulation", master, "http://127.0.0.1:8180"); err != nil {
		t.Fatal(err)
	}
	assertCleared(app)
	app = seeded()
	if err := configureProviders(app, "", master, ""); err != nil {
		t.Fatal(err)
	}
	assertCleared(app)
}
