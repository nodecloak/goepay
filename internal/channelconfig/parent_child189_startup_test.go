package channelconfig

import (
	adapter "goepay/internal/adapters/payment"
	"testing"
	"time"
)

func TestParentChild189ExpiredOriginPreservesClosedAliasChildRestorerAtStartup(t *testing.T) {
	stale, current := certificateChannelAt(t, time.Now().Add(-2*time.Hour)), certificateChannel(t)
	factory, err := NewParentChannelFactory(current, ParentBuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	current.ID = 18
	_, raw, err := factory.BuildSnapshot(18, current)
	if err != nil {
		t.Fatal(err)
	}
	config, certs, values := map[string]string{}, map[string]string{}, map[string]string{}
	for _, key := range []string{"appid", "appsecret"} {
		config[key] = key
		values[key] = current.Config[key]
	}
	for _, key := range []string{"app_cert_path", "alipay_cert_path", "root_cert_path"} {
		certs[key] = key
		values[key] = current.Certificates[key]
	}
	imp := Import{Version: 1, Channels: []Channel{stale}, PaymentSubchannelTemplates: []PaymentSubchannelTemplate{{ChannelID: 9, Revision: 1, ConfigSlots: config, CertificateSlots: certs}}}
	startup, err := PrepareParentStartup(imp, []adapter.ParentStartupAccount{{ChannelID: 18, SourceID: 9, PayType: "alipay", Snapshot: raw}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = BuildPaymentSubchannelFactories(startup.Import, BuildOptions{}); err == nil {
		t.Fatal("old direct startup unexpectedly restored expired source")
	}
	builder, ok := any(startup).(interface {
		BuildSubchannelFactories(BuildOptions) (map[int64]adapter.SubchannelFactory, error)
	})
	if !ok {
		t.Fatal("authenticated alias startup discarded registered child constructor origin")
	}
	factories, err := builder.BuildSubchannelFactories(BuildOptions{})
	if err != nil || factories[9] == nil {
		t.Fatal("closed child origin lost", err)
	}
	if _, err = factories[9].Build(values); err == nil {
		t.Fatal("restore-only origin became executable via replaced child slots")
	}
	alias, err := factories[9].(adapter.SubchannelAliasFactory).DeriveParentSubchannel(18, raw)
	if err != nil {
		t.Fatal("authenticated alias child constructor lost", err)
	}
	if _, err = alias.Build(values); err != nil {
		t.Fatal("valid alias child remains unusable after source expiry", err)
	}
}
