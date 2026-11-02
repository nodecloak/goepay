package merchantnotice

import (
	"goepay/internal/wxtransferlink231"
	"strings"
	"testing"
)

func TestConfirmationTemplate231UsesFixedSignedRouteAndPendingWording(t *testing.T) {
	in := templateInput80("wxtransfer")
	in.Params["confirm_path"] = wxtransferlink231.Path(make([]byte, 32), 231, in.At)
	cfg := WechatTemplateConfig{ID: "synthetic-confirm231", ConsoleURL: "https://console.example.invalid", Fields: map[string]string{"wxnotice_tpl_wxtransfer_money": "amount1"}}
	got, e := BuildWechatTemplate(in, cfg)
	if e != nil || got.URL != "https://console.example.invalid"+in.Params["confirm_path"] {
		t.Fatal("missing source-bound confirmation URL", got, e)
	}
	m, e := RenderWechat(in)
	if e != nil || !m.Valid() || !strings.Contains(m.Text, "待确认收款") || strings.Contains(m.Text, "到账") {
		t.Fatal("premature success wording", m.Text, e)
	}
	for _, bad := range []string{"https://evil.example/pay/wxtrans", "//evil.example/pay/wxtrans", "/pay/wxtrans?n=1&t=2&s=bad", "/merchant/dashboard", in.Params["confirm_path"] + "&s=duplicate"} {
		in.Params["confirm_path"] = bad
		if _, e := BuildWechatTemplate(in, cfg); e == nil {
			t.Fatal("unsafe confirmation redirect", bad)
		}
	}
}
