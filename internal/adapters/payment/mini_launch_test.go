package payment

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMiniLaunchMaterialsAreBoundToTrustedApplicationAndCompletion(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; t.Error("material generation called upstream") }))
	defer upstream.Close()
	metadata := &MiniProgramConfig{AppID: "wxapp", Path: "pages/pay/pay", OriginalID: "gh_synthetic"}
	wx, e := NewWechatV2(WechatV2Config{AppID: "wxapp", MerchantID: "mch", APIKey: "0123456789abcdef0123456789abcdef", Methods: []string{"jsapi"}, MiniProgram: metadata, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	metadata.Path = "pages/attacker" // constructor must retain an immutable copy.
	complete := "https://pay.example.invalid/oauth/mini/complete/wxpay/7/42/?state=" + strings.Repeat("a", 64)
	r := MiniLaunchRequest{TradeNo: "42", CompleteURL: complete, Amount: 101}
	result, e := wx.MiniLaunch(r)
	if e != nil || result.MaterialType != "wxapp" || result.ProviderTradeNo != "" {
		t.Fatal(result, e)
	}
	var data map[string]string
	if e = json.Unmarshal([]byte(result.Material), &data); e != nil || data["appId"] != "wxapp" || data["miniProgramId"] != "gh_synthetic" {
		t.Fatal(data, e)
	}
	page, e := url.Parse(data["path"])
	if e != nil || page.Path != "pages/pay/pay" || page.Query().Get("money") != "1.01" || page.Query().Get("url") != complete {
		t.Fatal("unbound mini page", data, e)
	}
	key := testKey(t)
	ali, e := NewAlipay(AlipayConfig{AppID: "aliapp", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"mini"}, MiniProgram: &MiniProgramConfig{Path: "pages/pay/pay"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	r.CompleteURL = strings.Replace(complete, "/wxpay/7/", "/alipay/8/", 1)
	result, e = ali.MiniLaunch(r)
	if e != nil || result.MaterialType != "urlscheme" || result.ProviderTradeNo != "" {
		t.Fatal(result, e)
	}
	scheme, e := url.Parse(result.Material)
	if e != nil || scheme.Scheme != "alipays" || scheme.Host != "platformapi" || scheme.Path != "/startapp" || scheme.Query().Get("appId") != "aliapp" {
		t.Fatal(result, e)
	}
	page, e = url.Parse(scheme.Query().Get("page"))
	if e != nil || page.Path != "pages/pay/pay" || page.Query().Get("url") != r.CompleteURL {
		t.Fatal(result, e)
	}
	if calls != 0 {
		t.Fatal("launch created payment upstream", calls)
	}
}

func TestMiniLaunchRejectsAmbiguousOrDifferentCompletionContext(t *testing.T) {
	if _, e := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: "0123456789abcdef0123456789abcdef", Methods: []string{"jsapi", "applet"}}); e == nil {
		t.Fatal("mini capability advertised without binding")
	}
	wx, e := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: "0123456789abcdef0123456789abcdef", Methods: []string{"jsapi"}, MiniProgram: &MiniProgramConfig{Path: "pages/pay/pay"}})
	if e != nil {
		t.Fatal(e)
	}
	base := "https://pay.example.invalid/oauth/mini/complete/wxpay/7/42/?state=" + strings.Repeat("a", 64)
	for _, raw := range []string{strings.Replace(base, "https:", "http:", 1), strings.Replace(base, "/wxpay/", "/alipay/", 1), strings.Replace(base, "/42/", "/43/", 1), strings.Replace(base, "/7/", "/0/", 1), strings.Replace(base, "/7/", "/07/", 1), strings.Replace(base, "/oauth/", "//oauth/", 1), base + "&state=another", base + "&redirect=https://other.invalid/", strings.Replace(base, "/42/", "/%34%32/", 1)} {
		if _, e = wx.MiniLaunch(MiniLaunchRequest{TradeNo: "42", CompleteURL: raw, Amount: 100}); !errors.Is(e, ErrInvalid) {
			t.Fatal("unsafe completion accepted", raw, e)
		}
	}
}
