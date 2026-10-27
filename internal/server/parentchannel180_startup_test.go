package server

import (
	"goepay/internal/channelconfig"
	"goepay/internal/payment"
	"testing"
)

func TestParentChannel180ActualTLSStartupAliasRestoresWithoutReplacingInvalidOrigin(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := t.Context()
	accounts, e := f.s.Store.ParentStartupAccounts(ctx)
	if e != nil || len(accounts) != 1 || accounts[0].SourceID != 8 {
		t.Fatal("startup did not authenticate actual alias", len(accounts), e)
	}
	oldFactory := f.s.ProviderFlow.ParentFactories[8].(*parent180TLSFactory)
	stale := oldFactory.Bootstrap()
	stale.Config["appkey"] = ""
	startup, e := channelconfig.PrepareParentStartup(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{stale}}, accounts)
	if e != nil {
		t.Fatal(e)
	}
	providers, e := startup.BuildProviders(channelconfig.BuildOptions{AllowLive: true})
	if e != nil || providers[8] != nil {
		t.Fatal("alias startup invented source account", e)
	}
	factories, e := startup.BuildFactories(channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}})
	if e != nil {
		t.Fatal(e)
	}
	// Keep the real closed factory and replace only its fixed transport with
	// this test's loopback mTLS server, exactly as the normal fixture does.
	factories[8] = &parent180TLSFactory{ParentChannelFactory: factories[8].(*channelconfig.ParentChannelFactory), endpoint: oldFactory.endpoint, roots: oldFactory.roots}
	mapping, e := startup.MerchantAllowlist(`{"1":[8]}`)
	if e != nil {
		t.Fatal(e)
	}
	flow := &payment.ProviderFlow{Store: f.s.Store, Providers: providers, MerchantChannels: mapping, AllowLive: true, CallbackBaseURL: f.s.ProviderFlow.CallbackBaseURL}
	if e = flow.InstallParentAccountFactories(factories); e != nil {
		t.Fatal("restore-only factory rejected without fake source SDK", e)
	}
	f.s.ProviderFlow = flow
	f.s.MerchantChannels = mapping
	if e = f.s.InstallParentRuntimeAuthorization(); e != nil {
		t.Fatal(e)
	}
	if out := parent180WxPay(t, f, setup.id, "parent180-restart-alias"); out["code"] != float64(1) || setup.creates.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("current alias failed after restart", out, setup.creates.Load(), setup.bad.Load())
	}
	if out := parent180WxPay(t, f, "8", "parent180-no-source-impersonation"); out["code"] == float64(1) || setup.creates.Load() != 1 {
		t.Fatal("invalid original source borrowed alias credentials", out, setup.creates.Load())
	}
	// Creation/copy templates remain permission origins. They contain no
	// fabricated financial fee/account binding for adoption of expired #8.
	if _, e = flow.ParentChannelTemplate(8, mapping); e != nil {
		t.Fatal("registered source template lost", e)
	}
	if _, _, e = factories[8].BootstrapSnapshot(8); e == nil {
		t.Fatal("invalid original became executable")
	}
}
