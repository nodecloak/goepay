package payment

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func alipaycodeChannel(appswitch string) PluginChannel {
	return PluginChannel{ID: 13, Plugin: "alipaycode", PayType: "alipay", Config: map[string]string{
		"appid": "2021app", "appsecret": alipaydTestPriv, "appmchid": "2088123456789012", "appswitch": appswitch,
	}}
}

func TestPluginAlipaycodeRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("alipaycode")
	if !ok || spec.Title != "支付宝免签约码支付" || !spec.Serves("alipay") {
		t.Fatal("spec", ok)
	}
	if _, e := BuildPluginChannel(alipaycodeChannel("1")); e != nil {
		t.Fatal(e)
	}
	// Bad UID (not 2088 / wrong length), missing key, missing UID.
	badUID := alipaycodeChannel("1")
	badUID.Config["appmchid"] = "123"
	missingKey := alipaycodeChannel("1")
	delete(missingKey.Config, "appsecret")
	missingUID := alipaycodeChannel("1")
	delete(missingUID.Config, "appmchid")
	for i, c := range []PluginChannel{badUID, missingKey, missingUID} {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginAlipaycodeMethods(t *testing.T) {
	p1, _ := BuildPluginChannel(alipaycodeChannel("1"))
	if m := p1.Capabilities().Methods; len(m) != 1 || m[0] != "native" {
		t.Fatal("confirm methods", m)
	}
	p0, _ := BuildPluginChannel(alipaycodeChannel("0"))
	if m := p0.Capabilities().Methods; len(m) != 0 {
		t.Fatal("plain-transfer must expose no method", m)
	}
}

func TestPluginAlipaycodeCreateRenderURL(t *testing.T) {
	p, _ := BuildPluginChannel(alipaycodeChannel("1"))
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "C100", Subject: "会员", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1500})
	if e != nil || res.MaterialType != "qrcode" {
		t.Fatal(res, e)
	}
	if !strings.HasPrefix(res.Material, alipaycodeRenderBase+"?formData=") {
		t.Fatal("render base", res.Material)
	}
	u, _ := url.Parse(res.Material)
	var form map[string]any
	if json.Unmarshal([]byte(u.Query().Get("formData")), &form) != nil {
		t.Fatal("formData not JSON")
	}
	if form["remark"] != "C100" || form["transAmount"] != "15.00" || form["productCode"] != "TRANSFER_TO_ALIPAY_ACCOUNT" {
		t.Fatal("formData fields", form)
	}
	payee, _ := form["payeeInfo"].(map[string]any)
	if payee["identity"] != "2088123456789012" || payee["identityType"] != "ALIPAY_USER_ID" {
		t.Fatal("payee", form["payeeInfo"])
	}
}

func TestPluginAlipaycodePlainTransferUnsupported(t *testing.T) {
	p, _ := BuildPluginChannel(alipaycodeChannel("0"))
	// Plain-transfer offers no method, so Create is refused by validation.
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "C1", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); e == nil {
		t.Fatal("plain-transfer create must fail")
	}
	// No signed callback exists for this plugin.
	v := p.(NotificationVerifier)
	if _, e := v.VerifyNotification(context.Background(), nil, []byte("a=1"), ExpectedPayment{TradeNo: "C1", Amount: 1}); !errors.Is(e, ErrUnsupported) {
		t.Fatal("notify must be unsupported", e)
	}
}
