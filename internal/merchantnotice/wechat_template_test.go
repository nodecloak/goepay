package merchantnotice

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func templateInput80(scene string) Input {
	return Input{Scene: scene, SiteName: "本地平台80", At: time.Date(2026, 10, 24, 1, 2, 3, 0, time.UTC), Params: map[string]string{"name": strings.Repeat("商品", 12), "money": "12.34", "realmoney": "12.00", "type": "微信支付", "out_trade_no": strings.Repeat("n", 40), "trade_no": "202610240001", "time": "2026-10-24 09:02:03", "account": "synthetic-account80", "user": "merchant80", "clientip": "192.0.2.80", "ipinfo": "本地测试", "title": "投诉原因", "content": "原投诉详情80", "msgmoney": "20.00"}}
}
func TestMerchantNotice80WechatTemplateUsesOriginalSceneFieldNamesAndAmounts(t *testing.T) {
	for _, tc := range []struct{ scene, field, want, path string }{
		{"order", "money", "¥12.34", "/merchant/trade/order"},
		{"settle", "type", "结算成功", "/merchant/settle"},
		{"settle", "realmoney", "¥12.00", "/merchant/settle"},
		{"login", "name", "本地平台80", "/merchant/dashboard"},
		{"complain", "reason", "原投诉详情80", "/merchant/dashboard"},
		{"balance", "money", "12.34", "/merchant/dashboard"},
		{"balance", "msg", "为避免造成订单失败，请及时充值", "/merchant/dashboard"},
		{"wxtransfer", "money", "¥12.34", "/merchant/dashboard"},
	} {
		t.Run(tc.scene+tc.field, func(t *testing.T) {
			got, err := BuildWechatTemplate(templateInput80(tc.scene), WechatTemplateConfig{ID: "synthetic-template80", ConsoleURL: "https://console.example.test", Fields: map[string]string{"wxnotice_tpl_" + tc.scene + "_" + tc.field: "thing1"}})
			if err != nil || got.Data["thing1"].Value != tc.want || got.URL != "https://console.example.test"+tc.path || got.TemplateID != "synthetic-template80" {
				t.Fatalf("source contract changed %+v %v", got, err)
			}
			message, messageErr := RenderWechat(templateInput80(tc.scene))
			if messageErr != nil || !message.Valid() {
				t.Fatal("template-only message unavailable", messageErr)
			}
		})
	}
}
func TestMerchantNotice80WechatTemplateClipsOriginalLimitsAndRejectsAmbiguousMappings(t *testing.T) {
	in := templateInput80("order")
	cfg := WechatTemplateConfig{ID: "synthetic-template80", ConsoleURL: "https://console.example.test", Fields: map[string]string{"wxnotice_tpl_order_name": "thing1", "wxnotice_tpl_order_outno": "character_string2"}}
	got, err := BuildWechatTemplate(in, cfg)
	if err != nil || utf8.RuneCountInString(got.Data["thing1"].Value) != 20 || got.Data["character_string2"].Value != strings.Repeat("n", 32) {
		t.Fatal("original length contract", got, err)
	}
	in.Params["out_trade_no"] = strings.Repeat("单", 12)
	got, err = BuildWechatTemplate(in, cfg)
	if err != nil || !utf8.ValidString(got.Data["character_string2"].Value) || len(got.Data["character_string2"].Value) > 32 {
		t.Fatal("split UTF8 at merchant number limit", got, err)
	}
	for _, fields := range []map[string]string{{"wxnotice_tpl_order_name": "thing1", "wxnotice_tpl_order_money": "thing1"}, {"wxnotice_tpl_login_user": "thing1"}, {"wxnotice_tpl_order_name": "<injected>"}, {}} {
		cfg.Fields = fields
		if _, err = BuildWechatTemplate(in, cfg); err == nil {
			t.Fatal("ambiguous/foreign template mapping accepted", fields)
		}
	}
	cfg.Fields = map[string]string{"wxnotice_tpl_order_money": "amount1"}
	cfg.ConsoleURL = "http://console.example.test"
	if _, err = BuildWechatTemplate(in, cfg); err == nil {
		t.Fatal("plaintext template destination allowed")
	}
}
