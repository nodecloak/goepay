package main

import (
	"bytes"
	"goepay/internal/channelconfig"
	"goepay/internal/server"
	"testing"
)

func TestConfiguredAuthorizationObservationIndependentDefaultOffAndStrictGate(t *testing.T) {
	master := bytes.Repeat([]byte{82}, 32)
	imp := channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "synthetic-app", "appmchid": "synthetic-mch", "appkey": "0123456789abcdef0123456789abcdef"}}}}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", oauthImportFile(t, master, imp))
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1000":[7]}`)
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "false")
	t.Setenv("GOEPAY_ENABLE_OAUTH", "false")
	t.Setenv("GOEPAY_ENABLE_IDENTITY_LOOKUP", "false")
	t.Setenv("GOEPAY_ENABLE_APPLICATION_AUTHORIZATION", "false")
	for _, gate := range []string{"", "false", "true"} {
		t.Setenv("GOEPAY_ENABLE_AUTHORIZATION_OBSERVATION", gate)
		app := &server.Server{AuthorizationObservationEnabled: true}
		if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e != nil || app.AuthorizationObservationEnabled != (gate == "true") || app.ProviderFlow.AllowLive {
			t.Fatal("independent read gate", gate, e)
		}
	}
	for _, gate := range []string{"TRUE", "yes", "1", "true "} {
		t.Setenv("GOEPAY_ENABLE_AUTHORIZATION_OBSERVATION", gate)
		app := &server.Server{AuthorizationObservationEnabled: true}
		if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e == nil || app.AuthorizationObservationEnabled {
			t.Fatal("invalid gate retained enabled state", gate, e)
		}
	}
	t.Setenv("GOEPAY_ENABLE_AUTHORIZATION_OBSERVATION", "true")
	app := &server.Server{AuthorizationObservationEnabled: true}
	if e := configureProviders(app, "configured", master, "http://pay.example.invalid"); e == nil || app.AuthorizationObservationEnabled {
		t.Fatal("insecure origin retained read gate", e)
	}
	app = &server.Server{AuthorizationObservationEnabled: true, Sandbox: true}
	if e := configureProviders(app, "simulation", master, "http://127.0.0.1:8180"); e != nil || app.AuthorizationObservationEnabled {
		t.Fatal("simulation retained external read gate", e)
	}
	app = &server.Server{AuthorizationObservationEnabled: true}
	if e := configureProviders(app, "", master, ""); e != nil || app.AuthorizationObservationEnabled {
		t.Fatal("disabled providers retained read gate", e)
	}
}
