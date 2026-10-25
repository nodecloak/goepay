package backoffice

func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "管理员异常通知 msgconfig_risk（0邮件/1群机器人/2 Telegram/3 QQ）", Value: "msgconfig_risk"},
				Option{Label: "管理员 Telegram chatID msgtelegram_chatid", Value: "msgtelegram_chatid"},
				Option{Label: "管理员 QQ机器人 OpenID qqbot_openid", Value: "qqbot_openid"},
				Option{Label: "管理员接收邮箱 mail_recv", Value: "mail_recv"},
				Option{Label: "管理员默认邮箱 mail_name", Value: "mail_name"})
		}
	}
	d.description += " 管理员异常通知按原msgconfig_risk选择；目标为空时按mail_recv、mail_name回退邮件。每次新通知读取当前设置，保存不发送测试消息。外发还要求worker明确启用；机器人URL、Bot token/secret与SMTP凭据仅来自受保护运行配置，不在通用设置保存。已领取通知不会因修改设置重发。"
	definitions["settings"] = d
}
