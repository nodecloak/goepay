package backoffice

func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "商户余额自动转账阈值 auto_settle_money（元，0关闭）", Value: "auto_settle_money"},
				Option{Label: "支付宝转账接口通道 transfer_alipay（ID，0关闭）", Value: "transfer_alipay"})
		}
	}
	d.description += " 商户余额自动转账使用auto_settle_money（元）与transfer_alipay（原通道ID）；按整余额、UID倒序最多5个。组settle_rate覆盖比例，仅全局最高手续费，不使用最低手续费/留存/普通每日次数。保存设置不签发计划或付款授权。"
	definitions["settings"] = d
	g := definitions["groups"]
	open := selectField("settle_open", "结算开关 settle_open", "0", "1", "2")
	open.Required = false
	open.Default = "0"
	open.Options = []Option{{Label: "继承默认", Value: "0"}, {Label: "允许结算", Value: "1"}, {Label: "关闭结算", Value: "2"}}
	g.fields = append(g.fields, open, text("settle_rate", "结算手续费率 settle_rate（%，空继承，0免手续费）", false, 16))
	g.description += " 余额阈值转账按有效会员组的settle_open/settle_rate执行；关闭组仍占原5人选择，不补位。settle_rate为百分比，最多两位小数。"
	definitions["groups"] = g
}
