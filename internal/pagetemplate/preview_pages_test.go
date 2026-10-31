package pagetemplate

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	qrcode "github.com/skip2/go-qrcode"
)

// TestWriteTemplatePreviewPages writes every built-in template as a static
// HTML page into GOEPAY_TEMPLATE_PREVIEW_DIR. web/scripts/template-previews.mjs
// then screenshots those pages into web/public/static/template-previews, the
// pictures shown on the 模板配置 cards and in the 应用市场. Regenerate both
// after changing a template:
//
//	GOEPAY_TEMPLATE_PREVIEW_DIR=<dir> go test -run TestWriteTemplatePreviewPages ./internal/pagetemplate
//	node web/scripts/template-previews.mjs <dir>
//
// It is skipped unless the directory is set.
func TestWriteTemplatePreviewPages(t *testing.T) {
	dir := os.Getenv("GOEPAY_TEMPLATE_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOEPAY_TEMPLATE_PREVIEW_DIR not set")
	}
	if e := qrcode.WriteFile("https://pay.example.com/checkout/2026102912345678", qrcode.Medium, 320, filepath.Join(dir, "demo-qr.png")); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"home", "pay", "qrpay"} {
		list, e := Templates(kind)
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range list {
			var b bytes.Buffer
			r := Resolved{Type: kind, Name: v.Name, Config: DefaultConfig(kind, v.Name)}
			switch kind {
			case "home":
				e = RenderHome(&b, r, HomeData{SiteName: "GOEPAY", Title: v.Title, Org: "示例科技有限公司", Email: "support@example.com", QQ: "10001", TestOpen: true, RegistrationOpen: true})
			case "pay":
				var tail string
				if tail, e = Open(&b, r, Page{Title: "支付收银台", TradeNo: "2026102912345678", Amount: "100.00", OrderName: "示例商品"}); e == nil {
					io.WriteString(&b, `<p>请使用支付宝扫描支付二维码。</p><img width="320" height="320" alt="支付二维码" src="demo-qr.png"><p>请在有效期内完成付款，付款结果以平台确认为准。</p>`+tail)
				}
			default:
				var tail string
				if tail, e = Open(&b, r, Page{Title: "向示例商户付款", MerchantName: "示例商户"}); e == nil {
					io.WriteString(&b, `<p>收款说明：向示例商户付款</p><form method="post" action="#"><label>金额（元）<input name="amount" type="number" value="100.00"></label><label>支付方式<select name="type"><option>微信支付</option><option>支付宝</option></select></label><button type="submit">前往支付</button></form>`+tail)
				}
			}
			if e != nil {
				t.Fatal(kind, v.Name, e)
			}
			if e = os.WriteFile(filepath.Join(dir, kind+"-"+v.Name+".html"), b.Bytes(), 0o644); e != nil {
				t.Fatal(e)
			}
			// Each home style also serves the service agreement page.
			if kind == "home" {
				b.Reset()
				if e = RenderAgreement(&b, r, HomeData{SiteName: "GOEPAY", Org: "示例科技有限公司", Email: "support@example.com", QQ: "10001", QQURL: "https://wpa.qq.com/msgrd?v=3&uin=10001", WeWork: "https://work.weixin.qq.com/kfid/kfc0000000000000000", QQGroup: "https://qm.qq.com/q/example", RegistrationOpen: true}); e != nil {
					t.Fatal(kind, v.Name, "agreement", e)
				}
				if e = os.WriteFile(filepath.Join(dir, kind+"-"+v.Name+"-agreement.html"), b.Bytes(), 0o644); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
}
