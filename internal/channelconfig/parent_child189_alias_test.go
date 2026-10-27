package channelconfig

import (
	adapter "goepay/internal/adapters/payment"
	"testing"
)

func TestParentChild189AliasDerivesClosedTemplateAndRestoresOriginalChild(t *testing.T) {
	parent := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "original-parent", "appmchid": "original-merchant", "appkey": "0123456789abcdef0123456789abcdef"}}
	factories, err := BuildPaymentSubchannelFactories(Import{Version: 1, Channels: []Channel{parent}, PaymentSubchannelTemplates: []PaymentSubchannelTemplate{{ChannelID: 7, Revision: 1, ConfigSlots: map[string]string{"appid": "app", "appmchid": "merchant"}}}}, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewParentChannelFactory(parent, ParentBuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := factory.BuildSnapshot(9, parent)
	if err != nil {
		t.Fatal(err)
	}
	derive, ok := factories[7].(interface {
		DeriveParentSubchannel(int64, []byte) (adapter.SubchannelFactory, error)
	})
	if !ok {
		t.Fatal("authenticated parent alias has no closed child constructor")
	}
	alias, err := derive.DeriveParentSubchannel(9, raw)
	if err != nil || alias.Schema().ParentID != 9 {
		t.Fatal("alias lost physical parent", err)
	}
	if _, err := derive.DeriveParentSubchannel(10, raw); err == nil {
		t.Fatal("different alias accepted snapshot")
	}
	child, err := alias.Snapshot(map[string]string{"app": "original-child", "merchant": "original-child-merchant"})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := alias.Restore(child)
	if err != nil || restored.Payment.(interface{ ApplicationID() string }).ApplicationID() != "original-child" {
		t.Fatal("alias child did not restore original SDK", err)
	}
	if _, err := factories[7].Restore(child); err == nil {
		t.Fatal("alias child impersonated origin parent")
	}
}
