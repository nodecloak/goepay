package payment

import (
	"errors"
	"testing"
)

func TestPluginYinyingtongUnsupported(t *testing.T) {
	spec, ok := LookupPlugin("yinyingtong")
	if !ok || spec.Title != "银盈通支付" || !spec.Serves("alipay") || !spec.Serves("bank") {
		t.Fatal("yinyingtong spec", ok)
	}
	// Build is intentionally unsupported: the payment notification decryption
	// format cannot be reproduced/verified from the available SDK.
	c := PluginChannel{ID: 14, Plugin: "yinyingtong", PayType: "alipay", Config: map[string]string{
		"appid": "A1", "appkey": "K1",
	}}
	if _, e := BuildPluginChannel(c); !errors.Is(e, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", e)
	}
}
