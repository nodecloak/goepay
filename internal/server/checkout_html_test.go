package server

import (
	"fmt"
	"html"
	"html/template"
	"strings"
	"testing"
)

func renderFragmentString(name string, data any) string {
	var b strings.Builder
	renderCheckoutFragment(&b, name, data)
	return b.String()
}

// Ordinary values render exactly the markup the pages produced before they
// moved to html/template.
func TestCheckoutFragmentsKeepTheirMarkup(t *testing.T) {
	for _, tc := range []struct {
		name, fragment string
		data           any
		want           string
	}{
		{"qr", "qr-image", struct{ Source, Label string }{"/checkout/2026/qr.png?expires=1793899660&token=a-b_c", "微信支付二维码"},
			`<img width="320" height="320" alt="微信支付二维码" style="display:block;width:248px;max-width:100%;height:auto;aspect-ratio:1;margin:18px auto;box-sizing:border-box" src="/checkout/2026/qr.png?expires=1793899660&amp;token=a-b_c">`},
		{"link", "link", struct{ Href, Text string }{"https://pay.example.com/checkout/2026?browser_launch=1&token=x", "在当前设备继续支付"},
			`<p><a href="https://pay.example.com/checkout/2026?browser_launch=1&amp;token=x">在当前设备继续支付</a></p>`},
		{"cashier", "cashier-form", struct {
			Payload, Token string
			Types          any
		}{"eyJ2Ijoxfq.c2ln-_", "dG9r", []cashierPayType{{Name: "alipay", ShowName: "支付宝"}, {Name: "wxpay", ShowName: "微信支付"}}},
			`<form method="post" action="/cashier?i=eyJ2Ijoxfq.c2ln-_&amp;t=dG9r"><fieldset><legend>支付方式</legend><label><input type="radio" name="type" value="alipay" checked> 支付宝</label><label><input type="radio" name="type" value="wxpay"> 微信支付</label></fieldset><button type="submit">立即支付</button></form>`},
		{"collection", "collection-form", struct {
			Title, Action, ExternalNo, FixedAmount string
			Options                                []checkoutOption
		}{"店铺收款", "/collect/abc?s=1&t=2", "bm9uY2U", "1.00", []checkoutOption{{"wxpay", "微信支付"}, {"alipay", "支付宝"}}},
			`<p>收款说明：店铺收款</p><form method="post" action="/collect/abc?s=1&amp;t=2"><input type="hidden" name="external_no" value="bm9uY2U"><label>金额（元）<input name="amount" type="number" min="0.01" max="1000000" step="0.01" required value="1.00" readonly></label><label>支付方式<select name="type"><option value="wxpay">微信支付</option><option value="alipay">支付宝</option></select></label><button type="submit">前往支付</button></form>`},
		{"scan region", "scan-region-warning", struct{ TradeNo, Continue string }{"202611060000000000000000001", "https://pay.example.com/checkout/202611060000000000000000001/scan-region-continue?expires=1&token=t"},
			`<!doctype html><html lang="zh-CN"><meta name="viewport" content="width=device-width,initial-scale=1"><title>支付安全提示</title><body><main><h1>支付安全提示</h1><p>扫码地区与下单地区不一致，请确认订单由本人发起，谨防陌生人代付诈骗。</p><p>订单：202611060000000000000000001</p><a href="https://pay.example.com/checkout/202611060000000000000000001/scan-region-continue?expires=1&amp;token=t">已核对订单，继续支付</a></main></body></html>`},
	} {
		if got := renderFragmentString(tc.fragment, tc.data); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}

func TestCheckoutFragmentsEscapeEveryValue(t *testing.T) {
	collection := renderFragmentString("collection-form", struct {
		Title, Action, ExternalNo, FixedAmount string
		Options                                []checkoutOption
	}{`<script>alert(1)</script>`, `javascript:alert(1)`, `"><img src=x>`, "", []checkoutOption{{`x" onclick="steal()`, `<b>pay</b>`}}})
	for _, raw := range []string{"<script>", "javascript:", `"><img`, `" onclick="`, "<b>"} {
		if strings.Contains(collection, raw) {
			t.Errorf("collection form emitted %q unescaped: %s", raw, collection)
		}
	}
	image := renderFragmentString("qr-image", struct{ Source, Label string }{"javascript:alert(1)", `"><svg onload=x>`})
	if strings.Contains(image, "javascript:") || strings.Contains(image, "<svg") {
		t.Errorf("QR image emitted an unsafe value: %s", image)
	}
}

// The provider checkout links and notes keep the markup produced by the former
// fmt/html.EscapeString code for ordinary values, including app-scheme links.
func TestProviderCheckoutFragmentsKeepTheirMarkup(t *testing.T) {
	old := func(format string, values ...any) string { return fmt.Sprintf(format, values...) }
	s := &Server{}
	weixin := "weixin://app/wx123/pay/?nonceStr=n&package=Sign%3DWXPay&partnerId=1&prepayId=wx2&sign=S&timeStamp=1"
	alipays := "alipays://platformapi/startApp?appId=20000125&orderSuffix=h5_route_token%3D%22x%22"
	qr := "/checkout/2026/qr.png?expires=1793899660&token=a-b_c"
	for _, tc := range []struct {
		name, fragment string
		data           any
		want           string
	}{
		{"status", "order-status", "pending", old("<p>平台订单状态：%s</p><p>以平台异步通知或查单结果为准。</p>", "pending")},
		{"qr", "provider-qr", struct{ Client, Label, Source, Material string }{"微信", "支付二维码", qr, "weixin://wxpay/bizpayurl?pr=abc"},
			old(`<p>请使用%s扫描%s。</p><img width="320" height="320" alt="%s" src="%s"><p>%s</p>`, "微信", "支付二维码", "支付二维码", html.EscapeString(qr), html.EscapeString("weixin://wxpay/bizpayurl?pr=abc"))},
		{"jump", "launch", launchLink{Href: mustLaunch(t, s, "https://wx.tenpay.com/cgi-bin/mmpayweb-bin/checkmweb?prepay_id=wx1&package=2"), Text: "继续支付"},
			old(`<a href="%s">继续支付</a>`, html.EscapeString("https://wx.tenpay.com/cgi-bin/mmpayweb-bin/checkmweb?prepay_id=wx1&package=2"))},
		{"app", "launch", launchLink{Href: mustLaunch(t, s, weixin), Text: "打开支付客户端", Note: "APP 支付结果以平台异步通知或查单结果为准。"},
			old(`<a href="%s">打开支付客户端</a><p>APP 支付结果以平台异步通知或查单结果为准。</p>`, html.EscapeString(weixin))},
		{"preauth", "launch", launchLink{Href: mustLaunch(t, s, alipays), Text: "打开支付宝授权支付", Note: "授权材料不代表付款成功；授权状态与实际扣款分别核对，最终以平台查单和异步通知为准。"},
			old(`<a href="%s">打开支付宝授权支付</a><p>授权材料不代表付款成功；授权状态与实际扣款分别核对，最终以平台查单和异步通知为准。</p>`, html.EscapeString(alipays))},
		{"wechat app", "jump", struct {
			Lead string
			Href template.URL
			Text string
		}{"请回到发起订单的应用完成支付；应用未自动打开时，可尝试下方入口。", mustLaunch(t, s, weixin), "打开微信支付"},
			old(`<p>请回到发起订单的应用完成支付；应用未自动打开时，可尝试下方入口。</p><a href="%s">打开微信支付</a>`, html.EscapeString(weixin))},
		{"mini params", "mini-params", `{"appId":"wx1","path":"pages/pay?x=1&y=<2>"}`,
			old("<p>请由商户客户端使用以下参数打开可信微信小程序：</p><pre>%s</pre>", html.EscapeString(`{"appId":"wx1","path":"pages/pay?x=1&y=<2>"}`))},
	} {
		if got := renderFragmentString(tc.fragment, tc.data); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}

func mustLaunch(t *testing.T, s *Server, raw string) template.URL {
	t.Helper()
	href, ok := s.trustedLaunchURL(raw)
	if !ok {
		t.Fatalf("launch URL refused: %s", raw)
	}
	return href
}

// Only https and the two payment-client schemes become links; script, data
// and credential-bearing URLs are refused, and text stays inert.
func TestProviderCheckoutLinksRefuseUnsafeURLs(t *testing.T) {
	s := &Server{}
	for _, raw := range []string{"javascript:alert(1)", "JaVaScRiPt:alert(1)", "data:text/html,<script>alert(1)</script>", "vbscript:x", "http://pay.example.com/x", "https://user:pass@pay.example.com/x", "https://pay.example.com/x#frag", "//pay.example.com/x", ""} {
		if _, ok := s.trustedLaunchURL(raw); ok {
			t.Errorf("unsafe launch URL accepted: %q", raw)
		}
	}
	got := renderFragmentString("provider-qr", struct{ Client, Label, Source, Material string }{"微信", "支付二维码", `/x" onerror="alert(1)`, `<script>alert(1)</script>`})
	if strings.Contains(got, "<script>") || strings.Contains(got, `" onerror="`) {
		t.Fatal("material escaped its context", got)
	}
}
