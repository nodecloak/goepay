package channelconfig

import (
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"testing"
)

func TestSubchannel168BTrustedTemplateSurvivesImportWithoutMakingParentASyntheticAccount(t *testing.T) {
	raw := []byte(`{"version":1,"channels":[{"id":7,"plugin":"wxpay","app_types":["1"],"config":{"appid":"parent-app","appmchid":"parent-mch","appkey":"0123456789abcdef0123456789abcdef"}}],"payment_subchannel_templates":[{"channel_id":"7","revision":"1","config_slots":{"appid":"child_app_id","appmchid":"child_merchant_id","appkey":"child_api_key"}}]}`)
	var imp Import
	if e := json.Unmarshal(raw, &imp); e != nil {
		t.Fatal(e)
	}
	out, e := json.Marshal(imp)
	if e != nil {
		t.Fatal(e)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(out, &fields) != nil || len(fields["payment_subchannel_templates"]) == 0 {
		t.Fatal("trusted executable child schema was silently discarded from the encrypted import model")
	}
	providers, e := BuildWithOptions(imp, BuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if len(providers) != 1 || providers[7] == nil {
		t.Fatal("child templates must retain the actual parent registry and never create synthetic provider IDs")
	}
}

func TestSubchannel168BFactoryKeepsTrustedSlotsImmutableAndUsesActualChildIdentity(t *testing.T) {
	parent := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "parent-app", "appmchid": "parent-mch", "appkey": "0123456789abcdef0123456789abcdef"}}
	imp := Import{Version: 1, Channels: []Channel{parent}, PaymentSubchannelTemplates: []PaymentSubchannelTemplate{{ChannelID: 7, Revision: 1, ConfigSlots: map[string]string{"appid": "child_app_id", "appmchid": "child_merchant_id", "appkey": "child_api_key"}}}}
	factories, e := BuildPaymentSubchannelFactories(imp, BuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	f := factories[7]
	values := map[string]string{"child_app_id": "child-app", "child_merchant_id": "child-mch", "child_api_key": "abcdef0123456789abcdef0123456789"}
	result, e := f.Build(values)
	if e != nil {
		t.Fatal(e)
	}
	if result.Payment.(interface{ ApplicationID() string }).ApplicationID() != "child-app" {
		t.Fatal("child must execute actual child app")
	}
	imp.PaymentSubchannelTemplates[0].ConfigSlots["appid"] = "child_merchant_id"
	parent.Config["appid"] = "mutated-parent"
	result, e = f.Build(values)
	if e != nil || result.Payment.(interface{ ApplicationID() string }).ApplicationID() != "child-app" {
		t.Fatal("caller mutated trusted factory after startup", e)
	}
}

func TestSubchannel168BOriginalAccountMaterialCanBeRestoredAfterParentTemplateRotation(t *testing.T) {
	parent := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "parent-app", "appmchid": "parent-mch", "appkey": "0123456789abcdef0123456789abcdef"}}
	makeFactory := func(c Channel) any {
		factories, e := BuildPaymentSubchannelFactories(Import{Version: 1, Channels: []Channel{c}, PaymentSubchannelTemplates: []PaymentSubchannelTemplate{{ChannelID: 7, Revision: 1, ConfigSlots: map[string]string{"appid": "app", "appmchid": "mch"}}}}, BuildOptions{})
		if e != nil {
			t.Fatal(e)
		}
		return factories[7]
	}
	type snapshots interface {
		Snapshot(map[string]string) ([]byte, error)
		Restore([]byte) (adapter.SubchannelExecution, error)
	}
	old, ok := makeFactory(parent).(snapshots)
	if !ok {
		t.Fatal("factory cannot retain original inherited payment credentials in an authenticated account version")
	}
	raw, e := old.Snapshot(map[string]string{"app": "original-child-app", "mch": "original-child-mch"})
	if e != nil {
		t.Fatal(e)
	}
	parent.Config["appkey"] = "abcdef0123456789abcdef0123456789"
	current := makeFactory(parent).(snapshots)
	result, e := current.Restore(raw)
	if e != nil || result.Payment.(interface{ ApplicationID() string }).ApplicationID() != "original-child-app" {
		t.Fatal("old account was replaced by current parent material", e)
	}
}

func TestSubchannel168BCurrentParentEstimateDoesNotReplaceFrozenAccountCredentials(t *testing.T) {
	parent := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "parent-app", "appmchid": "parent-mch", "appkey": "0123456789abcdef0123456789abcdef", "costrate": "0.30"}}
	build := func(c Channel) adapter.SubchannelFactory {
		fs, e := BuildPaymentSubchannelFactories(Import{Version: 1, Channels: []Channel{c}, PaymentSubchannelTemplates: []PaymentSubchannelTemplate{{ChannelID: 7, Revision: 1, ConfigSlots: map[string]string{"appid": "app", "appmchid": "mch"}}}}, BuildOptions{})
		if e != nil {
			t.Fatal(e)
		}
		return fs[7]
	}
	raw, e := build(parent).Snapshot(map[string]string{"app": "old-child-app", "mch": "old-child-mch"})
	if e != nil {
		t.Fatal(e)
	}
	parent.Config["costrate"] = "0.60"
	parent.Config["appkey"] = "abcdef0123456789abcdef0123456789"
	p, e := build(parent).Restore(raw)
	if e != nil {
		t.Fatal(e)
	}
	cost, known := p.Payment.(adapter.PaymentCostEstimator).PaymentCostEstimate()
	if !known || cost.RateUnits != 600000 || cost.AppID != "old-child-app" {
		t.Fatal("new route must inherit current declared parent cost estimate scoped to actual original child account", known, cost.RateUnits, cost.AppID)
	}
}
