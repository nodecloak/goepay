package backoffice

func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options, Option{Label: "admin_funds_authorization_enabled", Value: "admin_funds_authorization_enabled"})
		}
	}
	d.description += " admin_funds_authorization_enabled=true 才强制管理员逐动作资金授权；默认为false。启用后未设置独立资金密码的管理员不能发起新敏感资金意图，查询与商户签名接口保持原权限。"
	definitions["settings"] = d
}
