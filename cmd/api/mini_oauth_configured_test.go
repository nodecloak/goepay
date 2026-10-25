package main

import (
	"bytes"
	"goepay/internal/channelconfig"
	"goepay/internal/server"
	"testing"
)

func TestConfiguredMiniOAuthOnlyChannelAndGlobalOptInRemainOffline(t *testing.T) {
	master := bytes.Repeat([]byte{9}, 32)
	c := channelconfig.Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"2"}, Config: map[string]string{"appid": "wxapp", "appmchid": "mch", "appkey": "0123456789abcdef0123456789abcdef", "mini_secret": "synthetic-mini-secret", "mini_path": "pages/pay/pay", "enable_mini_launch": "true"}}
	path := oauthImportFile(t, master, channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{c}})
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", path)
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1000":[7]}`)
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "false")
	t.Setenv("GOEPAY_ENABLE_OAUTH", "")
	app := &server.Server{}
	if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e != nil || len(app.MiniOAuthProviders) != 0 {
		t.Fatal("mini OAuth enabled without global opt-in", e)
	}
	t.Setenv("GOEPAY_ENABLE_OAUTH", "true")
	if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e != nil || len(app.OAuthProviders) != 0 || len(app.MiniOAuthProviders) != 1 || app.MiniOAuthProviders[7].ApplicationID() != "wxapp" || app.ProviderFlow.AllowLive {
		t.Fatal("mini-only offline configuration unavailable", e)
	}
	t.Setenv("GOEPAY_ENABLE_OAUTH", "false")
	if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e != nil || len(app.MiniOAuthProviders) != 0 || len(app.OAuthProviders) != 0 {
		t.Fatal("disabled OAuth retained mini capability", e)
	}
	t.Setenv("GOEPAY_ENABLE_OAUTH", "true")
	c.Config["enable_mini_launch"] = "false"
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", oauthImportFile(t, master, channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{c}}))
	if e := configureProviders(&server.Server{}, "configured", master, "https://pay.example.invalid"); e == nil {
		t.Fatal("global opt-in bypassed mini channel opt-in")
	}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", "must-not-read-in-simulation")
	simulation := &server.Server{Sandbox: true}
	if e := configureProviders(simulation, "simulation", master, "http://127.0.0.1:8180"); e != nil || len(simulation.MiniOAuthProviders) != 0 {
		t.Fatal("simulation discovered mini credentials", e)
	}
}
