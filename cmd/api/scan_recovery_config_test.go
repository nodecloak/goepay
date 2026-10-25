package main

import (
	"bytes"
	"testing"

	provider "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/payment"
	"goepay/internal/server"
)

func TestScanRecovery78APIExplicitOptInAndFailureClearsOldExecution(t *testing.T) {
	master := bytes.Repeat([]byte{78}, 32)
	fixture := configuredApplicationAuthorizationImport(t)
	keys := fixture.ApplicationAuthorizationProfiles[0].Config
	imp := channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "alipay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "app78", "appmchid": "seller78", "appkey": keys["appkey"], "appsecret": keys["appsecret"], "enable_scan_pay": "true"}}}}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", oauthImportFile(t, master, imp))
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"78":[7]}`)
	t.Setenv("GOEPAY_ENABLE_OAUTH", "false")
	t.Setenv("GOEPAY_ENABLE_IDENTITY_LOOKUP", "false")
	t.Setenv("GOEPAY_ENABLE_APPLICATION_AUTHORIZATION", "false")
	t.Setenv("GOEPAY_ENABLE_AUTHORIZATION_OBSERVATION", "false")
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "true")
	t.Setenv("GOEPAY_ENABLE_PROVIDER_QUERIES", "true")
	for _, flag := range []string{"", "false", "true"} {
		t.Setenv("GOEPAY_ENABLE_SCAN_RECOVERY", flag)
		app := &server.Server{}
		if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e != nil || app.ProviderFlow == nil || app.ProviderFlow.ScanRecoveryEnabled != (flag == "true") || app.ProviderFlow.Providers[7].(provider.ScanCancellationProvider).CanCancelScan() != (flag == "true") {
			t.Fatal("incorrect independent recovery opt-in", flag, e)
		}
	}
	for _, mode := range []string{"ambiguous", "writes-disabled", "queries-disabled", "invalid-origin"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GOEPAY_ENABLE_SCAN_RECOVERY", "true")
			origin := "https://pay.example.invalid"
			switch mode {
			case "ambiguous":
				t.Setenv("GOEPAY_ENABLE_SCAN_RECOVERY", "1")
			case "writes-disabled":
				t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "false")
			case "queries-disabled":
				t.Setenv("GOEPAY_ENABLE_PROVIDER_QUERIES", "false")
			case "invalid-origin":
				origin = "http://pay.example.invalid"
			}
			app := &server.Server{ProviderFlow: &payment.ProviderFlow{AllowLive: true, ScanRecoveryEnabled: true}}
			if e := configureProviders(app, "configured", master, origin); e == nil || app.ProviderFlow.ScanRecoveryEnabled {
				t.Fatal("invalid configuration retained cancellation authority", mode, e)
			}
		})
	}
	t.Setenv("GOEPAY_ENABLE_SCAN_RECOVERY", "true")
	app := &server.Server{Sandbox: true, ProviderFlow: &payment.ProviderFlow{ScanRecoveryEnabled: true}}
	if e := configureProviders(app, "simulation", master, "http://127.0.0.1:8180"); e != nil || app.ProviderFlow.ScanRecoveryEnabled {
		t.Fatal("simulation retained configured recovery execution", e)
	}
}
