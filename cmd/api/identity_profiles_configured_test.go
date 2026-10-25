package main

import (
	"bytes"
	"goepay/internal/channelconfig"
	"goepay/internal/server"
	"testing"
)

func TestIndependentIdentityProfilesRequireExplicitGateAndRemainOffline(t *testing.T) {
	master := bytes.Repeat([]byte{64}, 32)
	channel := channelconfig.Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "wxpayapp", "appmchid": "local-mch", "appkey": "0123456789abcdef0123456789abcdef"}}
	profile := channelconfig.IdentityProfileSpec{ID: 8, Revision: 1, ChannelID: 7, Enabled: true, Provider: "wxpay", Kind: "mini", AppID: "wxidentityapp", AccountMode: "direct", Path: "pages/identity/index", MerchantIDs: channelconfig.IdentityMerchantIDs{1000}, Config: map[string]string{"appsecret": "synthetic-profile-secret"}}
	imp := channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{channel}, IdentityProfiles: []channelconfig.IdentityProfileSpec{profile}}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", oauthImportFile(t, master, imp))
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1000":[7]}`)
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "false")
	t.Setenv("GOEPAY_ENABLE_OAUTH", "true")
	t.Setenv("GOEPAY_ENABLE_IDENTITY_LOOKUP", "true")
	app := &server.Server{}
	if err := configureProviders(app, "configured", master, "https://pay.example.invalid"); err != nil || !app.IdentityProfileEnabled || len(app.IdentityProfiles) != 1 || app.ProviderFlow.AllowLive {
		t.Fatal("explicit independent profile must not require payment product or payment writes", err)
	}
	if app.IdentityProfiles[8].AppID != "wxidentityapp" {
		t.Fatal("identity application replaced by payment application")
	}
	t.Setenv("GOEPAY_ENABLE_IDENTITY_LOOKUP", "false")
	t.Setenv("GOEPAY_ENABLE_OAUTH", "false")
	if err := configureProviders(app, "configured", master, "https://pay.example.invalid"); err != nil || app.IdentityProfileEnabled || len(app.IdentityProfiles) != 0 {
		t.Fatal("disabled profile retained runtime capability", err)
	}
	t.Setenv("GOEPAY_ENABLE_IDENTITY_LOOKUP", "true")
	if err := configureProviders(&server.Server{}, "configured", master, "https://pay.example.invalid"); err == nil {
		t.Fatal("independent lookup ignored global OAuth gate")
	}
	t.Setenv("GOEPAY_ENABLE_OAUTH", "true")
	t.Setenv("GOEPAY_ENABLE_IDENTITY_LOOKUP", "yes")
	if err := configureProviders(&server.Server{}, "configured", master, "https://pay.example.invalid"); err == nil {
		t.Fatal("ambiguous identity enable accepted")
	}
}
