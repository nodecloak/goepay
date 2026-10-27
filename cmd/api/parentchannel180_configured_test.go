package main

import (
	"bytes"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/server"
	"strings"
	"testing"
)

func TestParentChannel180ConfiguredStartupPublishesRealFactoriesAndDynamicWriteGuard(t *testing.T) {
	master := bytes.Repeat([]byte{18}, 32)
	imp := channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "parent180-import", "appmchid": "parent180-import-mch", "appkey": "0123456789abcdef0123456789abcdef"}}}}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", oauthImportFile(t, master, imp))
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1000":[7]}`)
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "true")
	t.Setenv("GOEPAY_ENABLE_SCAN_RECOVERY", "false")
	t.Setenv("GOEPAY_ENABLE_OAUTH", "false")
	t.Setenv("GOEPAY_ENABLE_IDENTITY_LOOKUP", "false")
	t.Setenv("GOEPAY_ENABLE_APPLICATION_AUTHORIZATION", "false")
	t.Setenv("GOEPAY_ENABLE_AUTHORIZATION_OBSERVATION", "false")
	app := &server.Server{}
	if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e != nil {
		t.Fatal(e)
	}
	factory := app.ProviderFlow.ParentFactories[7]
	if factory == nil || factory.OriginID() != 7 || factory.PayType() != "wxpay" {
		t.Fatal("configured runtime never registered actual parent factory")
	}
	appid, mch, key := "parent180-new", "parent180-new-mch", "abcdef0123456789abcdef0123456789"
	appTypes := []string{"1"}
	execution, _, e := factory.NewSnapshot(8, adapter.ParentChannelAccountChange{AppTypes: &appTypes, Config: map[string]adapter.ParentChannelValueEdit{"appid": {Operation: "replace", Value: &appid}, "appmchid": {Operation: "replace", Value: &mch}, "appkey": {Operation: "replace", Value: &key}}})
	if e != nil {
		t.Fatal(e)
	}
	v, ok := execution.Payment.(adapter.CreateValidator)
	if !ok {
		t.Fatal("real validator lost")
	}
	request := adapter.CreateRequest{TradeNo: "synthetic180-offline", Subject: "test", Method: "native", NotifyURL: "https://pay.example.invalid/callback", ClientIP: "192.0.2.1", Amount: 100}
	if e = v.ValidateCreate(request); e != nil {
		t.Fatal("positive pure validation failed", e)
	}
	app.ProviderFlow.AllowLive = false
	if e = v.ValidateCreate(request); !errors.Is(e, channelconfig.ErrLiveDisabled) {
		t.Fatal("already restored parent adapter ignored current root write gate", e)
	}
}

func TestParentChannel180ConfiguredOAuthFactoryCannotKeepRevokedGlobalProfileGate(t *testing.T) {
	master := bytes.Repeat([]byte{18}, 32)
	imp := channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "wxpay", AppTypes: []string{"2"}, Config: map[string]string{"appid": "parent180-import", "appmchid": "parent180-import-mch", "appkey": "0123456789abcdef0123456789abcdef", "enable_web_oauth": "true", "oauth_appsecret": "synthetic-original-oauth-secret"}}}}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", oauthImportFile(t, master, imp))
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1000":[7]}`)
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "true")
	t.Setenv("GOEPAY_ENABLE_SCAN_RECOVERY", "false")
	t.Setenv("GOEPAY_ENABLE_OAUTH", "true")
	t.Setenv("GOEPAY_ENABLE_IDENTITY_LOOKUP", "false")
	t.Setenv("GOEPAY_ENABLE_APPLICATION_AUTHORIZATION", "false")
	t.Setenv("GOEPAY_ENABLE_AUTHORIZATION_OBSERVATION", "false")
	app := &server.Server{}
	if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e != nil {
		t.Fatal(e)
	}
	factory := app.ProviderFlow.ParentFactories[7]
	if factory == nil {
		t.Fatal("factory absent")
	}
	execution, _, e := factory.BootstrapSnapshot(7)
	if e != nil || execution.Web == nil {
		t.Fatal("real OAuth construction absent", e)
	}
	if _, e = execution.Web.OAuthAuthorizationURL(strings.Repeat("a", 32), "https://pay.example.invalid/callback"); e != nil {
		t.Fatal(e)
	}
	app.OAuthProviders = nil
	if _, e = execution.Web.OAuthAuthorizationURL(strings.Repeat("a", 32), "https://pay.example.invalid/callback"); !errors.Is(e, channelconfig.ErrLiveDisabled) {
		t.Fatal("restored profile revived revoked global OAuth", e)
	}
}
