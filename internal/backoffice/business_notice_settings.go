package backoffice

import "goepay/internal/businessnotice"

func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			for _, setting := range businessnotice.PlatformSettings() {
				d.fields[i].Options = append(d.fields[i].Options, Option{Label: setting.Label, Value: setting.Key})
			}
		}
	}
	d.description += " 管理员业务提醒使用原0关闭/1邮件/2机器人/4Telegram/5QQ，站内转账共用msgconfig_domain；与msgconfig_risk不同。保存不发送消息。"
	definitions["settings"] = d
}
