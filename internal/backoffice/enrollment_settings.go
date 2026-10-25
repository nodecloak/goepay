package backoffice

func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options, Option{Label: "注册必须邀请码", Value: "registration_invite_required"})
		}
	}
	d.description += " registration_open 缺省关闭；registration_invite_required=true 时邮箱注册必须消费有效邀请码。验证码邮件发送器需独立配置，未配置时返回不可用。"
	definitions["settings"] = d
}
