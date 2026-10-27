package backoffice

func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "连续通知失败关闭新支付权限 auto_check_notify（0关闭/1开启）", Value: "auto_check_notify"},
				Option{Label: "连续失败订单数量 check_notify_count（0关闭，最多1000）", Value: "check_notify_count"},
				Option{Label: "支付权限关闭邮件 check_notify_notice（0关闭/1开启）", Value: "check_notify_notice"})
		}
	}
	d.description += " 通知失败管控只在真实自动通知重试后生效；原定时通知任务必须已明确接管且开启。只计有原失败来源证明的最近订单，不把配置或准入错误算成商户回调失败。关闭新支付权限保留原订单回执查询和账务。邮件仍需原worker显式启用及受保护SMTP配置。"
	definitions["settings"] = d
}
