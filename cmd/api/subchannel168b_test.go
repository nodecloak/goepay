package main

import (
	"bytes"
	"goepay/internal/channelconfig"
	"goepay/internal/server"
	"testing"
)

func TestSubchannel168BConfiguredFactoriesAreConsumedWithoutSyntheticProviders(t *testing.T) {
	master := bytes.Repeat([]byte{2}, 32)
	imp := channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "parent-app", "appmchid": "parent-mch", "appkey": "0123456789abcdef0123456789abcdef"}}}, PaymentSubchannelTemplates: []channelconfig.PaymentSubchannelTemplate{{ChannelID: 7, Revision: 1, ConfigSlots: map[string]string{"appid": "child_app_id", "appmchid": "child_merchant_id", "appkey": "child_api_key"}}}}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", oauthImportFile(t, master, imp))
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1000":[7]}`)
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "")
	app := &server.Server{}
	if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e != nil {
		t.Fatal(e)
	}
	if len(app.ProviderFlow.SubchannelFactories) != 1 || app.ProviderFlow.SubchannelFactories[7] == nil || len(app.ProviderFlow.Providers) != 1 {
		t.Fatal("authenticated child template is not actually consumed by configured payment execution")
	}
}
