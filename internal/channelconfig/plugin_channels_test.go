package channelconfig

import (
	"context"
	adapter "goepay/internal/adapters/payment"
	"testing"
)

func epayOrigin(id int64, payType string) Channel {
	return Channel{ID: id, Plugin: "epay", Config: map[string]string{"pay_type": payType, "appurl": "https://up.example/", "appid": "1000", "appkey": "synthetic-key", "appswitch": "0", "costrate": "0.6"}}
}

func TestPluginChannelBuildsThroughFactory(t *testing.T) {
	out, e := BuildWithOptions(Import{Version: 1, Channels: []Channel{epayOrigin(31, "alipay"), epayOrigin(32, "wxpay")}}, BuildOptions{AllowLive: true})
	if e != nil {
		t.Fatal(e)
	}
	if out[31].ID() != "alipay" || out[32].ID() != "wxpay" {
		t.Fatal("plugin providers must report their pay type", out[31].ID(), out[32].ID())
	}
	if name := out[31].(interface{ PluginName() string }).PluginName(); name != "epay" {
		t.Fatal(name)
	}
	if got := out[31].(interface{ NotificationMethods() []string }).NotificationMethods(); len(got) != 2 {
		t.Fatal("methods not forwarded", got)
	}
	if status, _, body := out[31].(interface {
		NotificationAck(error) (int, string, string)
	}).NotificationAck(nil); status != 200 || body != "success" {
		t.Fatal("ack not forwarded", status, body)
	}
	if _, ok := out[31].(interface {
		PaymentCostEstimate() (adapter.PaymentCostEstimate, bool)
	}); !ok {
		t.Fatal("cost estimate lost")
	}
	// Live writes stay behind the operator switch.
	off, _ := BuildWithOptions(Import{Version: 1, Channels: []Channel{epayOrigin(33, "alipay")}}, BuildOptions{})
	if _, e := off[33].Create(context.Background(), adapter.CreateRequest{TradeNo: "T", Subject: "x", Method: "page", NotifyURL: "https://a.example/n", Amount: 1}); e != ErrLiveDisabled {
		t.Fatal("live gate", e)
	}
	for name, c := range map[string]Channel{
		"unserved pay type": epayOrigin(34, "paypal"),
		"unknown plugin":    {ID: 35, Plugin: "nosuchplugin", Config: map[string]string{"pay_type": "alipay"}},
		"sharing":           func() Channel { c := epayOrigin(36, "alipay"); c.Config["enable_channel_sharing"] = "true"; return c }(),
		"direct settle":     func() Channel { c := epayOrigin(37, "alipay"); c.Config["mode"] = "1"; return c }(),
	} {
		if _, e := BuildWithOptions(Import{Version: 1, Channels: []Channel{c}}, BuildOptions{}); e == nil {
			t.Fatal("accepted", name)
		}
	}
}

func TestPluginParentTemplateKeepsPayTypeAndLabels(t *testing.T) {
	f, e := NewParentChannelFactory(epayOrigin(41, "wxpay"), ParentBuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if f.PayType() != "wxpay" {
		t.Fatal(f.PayType())
	}
	labels := map[string]adapter.ParentChannelField{}
	for _, field := range f.Fields() {
		labels[field.Name] = field
		if field.Name == "pay_type" {
			t.Fatal("pay type must not be editable")
		}
	}
	if labels["appurl"].Label != "接口地址" || !labels["appkey"].Secret || len(labels["appswitch"].Options) != 2 {
		t.Fatal(labels)
	}
	value := func(s string) *string { return &s }
	exec, raw, e := f.NewSnapshot(42, adapter.ParentChannelAccountChange{Config: map[string]adapter.ParentChannelValueEdit{
		"appurl": {Operation: "replace", Value: value("https://up2.example/")},
		"appid":  {Operation: "replace", Value: value("2000")},
		"appkey": {Operation: "replace", Value: value("another-key")},
	}})
	if e != nil || exec.Payment == nil || exec.Payment.ID() != "wxpay" || len(raw) == 0 {
		t.Fatal(e)
	}
	if _, _, e = f.ChangeSnapshot(42, raw, adapter.ParentChannelAccountChange{Config: map[string]adapter.ParentChannelValueEdit{"pay_type": {Operation: "replace", Value: value("alipay")}}}); e == nil {
		t.Fatal("pay type change accepted")
	}
	if _, _, e = f.ChangeSnapshot(42, raw, adapter.ParentChannelAccountChange{Config: map[string]adapter.ParentChannelValueEdit{"sub_mchid": {Operation: "replace", Value: value("x")}}}); e == nil {
		t.Fatal("unknown key accepted")
	}
	if _, e := NewParentChannelFactory(epayOrigin(43, "paypal"), ParentBuildOptions{}); e == nil {
		t.Fatal("origin with an unserved pay type accepted")
	}
}
