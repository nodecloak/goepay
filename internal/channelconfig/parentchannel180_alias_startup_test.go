package channelconfig

import (
	adapter "goepay/internal/adapters/payment"
	"testing"
	"time"
)

func TestParentChannel180ExpiredAliasOriginRetainsOnlyRestorerAndNeverBorrowsAliasAccount(t *testing.T) {
	stale := certificateChannelAt(t, time.Now().Add(-2*time.Hour))
	current := certificateChannel(t)
	if _, e := NewParentChannelFactory(stale, ParentBuildOptions{}); e == nil {
		t.Fatal("expired original certificate accepted")
	}
	f, e := NewParentChannelFactory(current, ParentBuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	current.ID = 18
	_, raw, e := f.BuildSnapshot(18, current)
	if e != nil {
		t.Fatal(e)
	}
	imp := Import{Version: 1, Channels: []Channel{stale}}
	startup, e := PrepareParentStartup(imp, []adapter.ParentStartupAccount{{ChannelID: 18, SourceID: 9, PayType: "alipay", Snapshot: raw}})
	if e != nil {
		t.Fatal("current alias could not recover without old expired bootstrap", e)
	}
	providers, e := startup.BuildProviders(BuildOptions{})
	if e != nil || providers[9] != nil || len(providers) != 0 {
		t.Fatal("alias account impersonated original source", len(providers), e)
	}
	factories, e := startup.BuildFactories(ParentBuildOptions{})
	if e != nil || factories[9] == nil {
		t.Fatal(e)
	}
	if _, _, e = factories[9].BootstrapSnapshot(9); e == nil {
		t.Fatal("expired source became executable")
	}
	execution, e := factories[9].Restore(18, raw)
	if e != nil || execution.Payment == nil {
		t.Fatal("authenticated alias did not restore", e)
	}
	if _, e = startup.MerchantAllowlist(`{"1":[9]}`); e != nil {
		t.Fatal("operator source permission lost", e)
	}
	if _, e = startup.MerchantAllowlist(`{"1":[18]}`); e == nil {
		t.Fatal("alias became operator authorization origin")
	}
	for _, records := range [][]adapter.ParentStartupAccount{nil, {{ChannelID: 18, SourceID: 8, PayType: "alipay", Snapshot: raw}}, {{ChannelID: 18, SourceID: 9, PayType: "wxpay", Snapshot: raw}}, {{ChannelID: 19, SourceID: 9, PayType: "alipay", Snapshot: raw}}} {
		if _, e = PrepareParentStartup(imp, records); e == nil {
			t.Fatal("unproven alias silently repaired expired origin")
		}
	}
}
