package channelconfig

import (
	"encoding/json"
	"testing"
)

func TestSubchannel168BSharedLogicalSlotRetainsOriginalPlaceholderMeaning(t *testing.T) {
	p := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "parent-app", "appmchid": "parent-mch", "appkey": "0123456789abcdef0123456789abcdef", "mini_appid": "parent-mini"}}
	imp := Import{Version: 1, Channels: []Channel{p}, PaymentSubchannelTemplates: []PaymentSubchannelTemplate{{ChannelID: 7, Revision: 1, ConfigSlots: map[string]string{"appid": "shared_app", "mini_appid": "shared_app", "appmchid": "merchant"}}}}
	fs, e := BuildPaymentSubchannelFactories(imp, BuildOptions{})
	if e != nil {
		t.Fatal("same public placeholder cannot be reused by original fields", e)
	}
	if len(fs[7].Schema().Fields) != 2 {
		t.Fatal("same placeholder duplicated its input")
	}
	if _, e = fs[7].Build(map[string]string{"shared_app": "child-app", "merchant": "child-mch"}); e != nil {
		t.Fatal(e)
	}
	imp.PaymentSubchannelTemplates[0].ConfigSlots["appkey"] = "shared_app"
	if _, e = BuildPaymentSubchannelFactories(imp, BuildOptions{}); e == nil {
		t.Fatal("secret/public placeholder collision accepted")
	}
}
func TestSubchannel168BExecutionCannotBeJSONProjected(t *testing.T) {
	p := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "parent-app", "appmchid": "parent-mch", "appkey": "0123456789abcdef0123456789abcdef"}}
	fs, e := BuildPaymentSubchannelFactories(Import{Version: 1, Channels: []Channel{p}, PaymentSubchannelTemplates: []PaymentSubchannelTemplate{{ChannelID: 7, Revision: 1, ConfigSlots: map[string]string{"appid": "app"}}}}, BuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	v, e := fs[7].Build(map[string]string{"app": "child-app"})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(v)
	if e != nil || string(raw) != "{}" {
		t.Fatal("private execution gained a public JSON surface", string(raw), e)
	}
}
