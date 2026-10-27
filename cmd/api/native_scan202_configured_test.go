package main

import (
	"bytes"
	"reflect"
	"testing"

	"goepay/internal/channelconfig"
	"goepay/internal/server"
)

func TestNativeScan202ConfiguredCandidateDoesNotEnableOAuthOrWritesAndClearsOnOtherModes(t *testing.T) {
	master := bytes.Repeat([]byte{202}, 32)
	channel := channelconfig.Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "wxpayapp", "appmchid": "local-mch", "appkey": "0123456789abcdef0123456789abcdef"}}
	profile := channelconfig.IdentityProfileSpec{ID: 8, Revision: 1, ChannelID: 7, Enabled: true, Provider: "wxpay", Kind: "web", AppID: "wxscan202", AccountMode: "direct", MerchantIDs: channelconfig.IdentityMerchantIDs{1000}, Config: map[string]string{"appsecret": "local-scan202-synthetic-secret"}}
	imp := channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{channel}, IdentityProfiles: []channelconfig.IdentityProfileSpec{profile}}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", oauthImportFile(t, master, imp))
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1000":[7]}`)
	for _, key := range []string{"GOEPAY_ENABLE_PROVIDER_WRITES", "GOEPAY_ENABLE_OAUTH", "GOEPAY_ENABLE_IDENTITY_LOOKUP", "GOEPAY_ENABLE_APPLICATION_AUTHORIZATION"} {
		t.Setenv(key, "false")
	}
	app := &server.Server{}
	if err := configureProviders(app, "configured", master, "https://pay.example.invalid/"); err != nil {
		t.Fatal(err)
	}
	// Reflection keeps this regression compilable before the new runtime field
	// exists. The behavior is still exercised through the real startup builder.
	profiles := reflect.ValueOf(app).Elem().FieldByName("ScanJSProfiles202")
	origin := reflect.ValueOf(app).Elem().FieldByName("ScanJSSiteOrigin202")
	if !profiles.IsValid() || profiles.Kind() != reflect.Map || profiles.Len() != 1 || !origin.IsValid() || origin.String() != "https://pay.example.invalid" {
		t.Fatal("configured startup did not publish the independent native-scan candidate and normalized site origin")
	}
	if app.IdentityProfileEnabled || app.ApplicationAuthorizationEnabled || len(app.IdentityProfiles)+len(app.ApplicationAuthorizationProfiles)+len(app.OAuthProviders)+len(app.MiniOAuthProviders) != 0 || app.ProviderFlow == nil || app.ProviderFlow.AllowLive {
		t.Fatal("native candidate construction enabled another authority or payment writes")
	}
	app.Sandbox = true
	if err := configureProviders(app, "simulation", master, "http://127.0.0.1:8187"); err != nil {
		t.Fatal(err)
	}
	profiles = reflect.ValueOf(app).Elem().FieldByName("ScanJSProfiles202")
	origin = reflect.ValueOf(app).Elem().FieldByName("ScanJSSiteOrigin202")
	if !profiles.IsNil() || origin.String() != "" {
		t.Fatal("simulation retained real JSSDK credentials or site origin")
	}
}
