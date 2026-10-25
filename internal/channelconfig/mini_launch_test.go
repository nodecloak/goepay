package channelconfig

import (
	"errors"
	provider "goepay/internal/adapters/payment"
	"slices"
	"strings"
	"testing"
)

func TestMiniLaunchRequiresExplicitTrustedApplicationAndPath(t *testing.T) {
	c := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"2"}, Config: map[string]string{"appid": "wxapp", "appmchid": "mch", "appkey": "0123456789abcdef0123456789abcdef", "mini_secret": "synthetic-mini-secret"}}
	providers, e := Build(Import{Version: 1, Channels: []Channel{c}}, false)
	if e != nil || slices.Contains(providers[7].Capabilities().Methods, "applet") {
		t.Fatal("mini enabled by default", e)
	}
	c.Config["enable_mini_launch"] = "true"
	if _, e = Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
		t.Fatal("mini launch without trusted path accepted")
	}
	c.Config["mini_path"] = "pages/pay/pay"
	providers, e = Build(Import{Version: 1, Channels: []Channel{c}}, false)
	if e != nil || !slices.Contains(providers[7].Capabilities().Methods, "applet") {
		t.Fatal("explicit mini product missing", e)
	}
	if _, e = providers[7].(provider.MiniLaunchProvider).MiniLaunch(provider.MiniLaunchRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("mini launch bypassed write switch", e)
	}
	for _, path := range []string{"../pay", "pages/../pay", "pages/pay?appid=other", "pages/%2f/pay", "pages\\pay"} {
		c.Config["mini_path"] = path
		if _, e = Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
			t.Fatal("untrusted mini path accepted", path)
		}
	}
	c.Config["mini_path"] = "pages/pay/pay"
	c.Config["mini_appid"] = "foreign-app"
	if _, e = Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
		t.Fatal("cross application mini configured")
	}
	delete(c.Config, "mini_appid")
	c.AppTypes = []string{"1"}
	if _, e = Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
		t.Fatal("mini launch without JSAPI advertised")
	}
}

func TestMiniLaunchAlipayCertificateRemainsExplicit(t *testing.T) {
	c := certificateChannel(t)
	c.AppTypes = []string{"7"}
	c.Config["enable_mini_launch"] = "true"
	c.Config["mini_path"] = "pages/pay/index"
	providers, e := Build(Import{Version: 1, Channels: []Channel{c}}, true)
	if e != nil {
		t.Fatal(e)
	}
	p := providers[c.ID].(provider.MiniLaunchProvider)
	metadata, ok := p.MiniProgram()
	if !ok || metadata.AppID != c.Config["appid"] {
		t.Fatal("certificate mini application was not pinned", metadata)
	}
	result, e := p.MiniLaunch(provider.MiniLaunchRequest{TradeNo: "42", Amount: 100, CompleteURL: "https://pay.example.invalid/oauth/mini/complete/alipay/9/42/?state=" + strings.Repeat("a", 64)})
	if e != nil || result.MaterialType != "urlscheme" {
		t.Fatal(result, e)
	}
	c.Config["mini_program_id"] = "gh_wrong_provider"
	if _, e := Build(Import{Version: 1, Channels: []Channel{c}}, true); e == nil {
		t.Fatal("Wechat metadata accepted for Alipay mini")
	}
}
