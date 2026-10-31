package devdoc

import (
	"html"
	"strings"
	"testing"
)

// These checks preserve externally important distinctions: the server is Go,
// legacy path names remain wire-compatible, and an accepted operation is not
// represented as a final payment/refund/transfer outcome.
func TestGo217ContractPagesDescribeCurrentServer(t *testing.T) {
	for _, slug := range []string{"pay_submit", "pay_create", "pay_query", "pay_notify", "pay_refund", "pay_refundquery", "pay_close", "merchant_info", "merchant_orders", "transfer_submit", "transfer_query", "transfer_balance", "paytype", "v1_legacy_api"} {
		t.Run(slug, func(t *testing.T) {
			out := render(t, slug, testSite)
			_, article, _ := strings.Cut(out, `<article class="doc-article">`)
			article, _, _ = strings.Cut(article, "</article>")
			if !strings.Contains(article, "Go 版") {
				t.Fatal("article must identify the current Go implementation")
			}
			for _, obsolete := range []string{"http://www.pay.com", "notify_url.php", "return_url.php", "默认为RSA", "10位整数"} {
				if strings.Contains(article, obsolete) {
					t.Errorf("obsolete or inaccurate contract wording: %s", obsolete)
				}
			}
		})
	}
}

func TestGo217RefundAndTransferOutcomeContracts(t *testing.T) {
	refund := html.UnescapeString(render(t, "pay_refundquery", testSite))
	for _, required := range []string{`"code": -1`, "refund processing", "不含", "终态"} {
		if !strings.Contains(refund, required) {
			t.Errorf("refund query must explain pending response: %s", required)
		}
	}
	transfer := render(t, "transfer_submit", testSite)
	if strings.Contains(transfer, "QQ钱包") || !strings.Contains(transfer, "query_required") || !strings.Contains(transfer, "out_biz_no") {
		t.Fatal("transfer contract must document supported rails and original-intent recovery")
	}
	orders := render(t, "merchant_orders", testSite)
	if !strings.Contains(orders, "数组") || !strings.Contains(orders, "不参与") || !strings.Contains(orders, "HTTPS") {
		t.Fatal("list response must identify the unsigned array and HTTPS boundary")
	}
	legacy := render(t, "v1_legacy_api", testSite)
	for _, path := range []string{"submit.php", "mapi.php", "api.php"} {
		if !strings.Contains(legacy, path) || !strings.Contains(legacy, "V1 接口") {
			t.Errorf("V1 interface route missing: %s", path)
		}
	}
}
