package pagetemplate

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
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
				if e = Open(&b, r, Page{Title: "订单支付", TradeNo: "2026102912345678", Amount: "100.00", OrderName: "示例商品", MerchantName: "示例商户"}); e == nil {
					io.WriteString(&b, "<p>请使用支付宝或微信扫码完成付款。</p>")
					e = Close(&b)
				}
			default:
				if e = Open(&b, r, Page{Title: "扫码付款", MerchantName: "示例商户"}); e == nil {
					io.WriteString(&b, "<p>请输入付款金额，确认后完成付款。</p>")
					e = Close(&b)
				}
			}
			if e != nil {
				t.Fatal(kind, v.Name, e)
			}
			if e = os.WriteFile(filepath.Join(dir, kind+"-"+v.Name+".html"), b.Bytes(), 0o644); e != nil {
				t.Fatal(e)
			}
		}
	}
}
