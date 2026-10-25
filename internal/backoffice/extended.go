package backoffice

// The following resources persist operational configuration or local case data.
// Provider submission, package installation, and bank verification are explicit
// separate operations; storing these records does not report those as completed.
func init() {
	definitions["pay-types"] = definition{title: "支付方式", description: "本地支付方式资料，不自动创建支付适配器。", fields: []Field{text("name", "展示名称", true, 100), selectField("type", "类型", "alipay", "wxpay", "sandbox"), flag("enabled", "启用资料", true)}}
	definitions["pay-rolls"] = payRollDefinition()
	definitions["weixin-apps"] = definition{title: "公众号小程序", description: "保存公开应用标识；密钥通过受控配置加载。", fields: []Field{text("name", "名称", true, 100), text("app_id", "App ID", true, 100), selectField("kind", "应用类型", "official_account", "mini_program"), flag("enabled", "启用资料", true)}}
	definitions["onboarding-channels"] = definition{title: "进件渠道配置", description: "配置进件资料字段；向渠道提交材料需要独立已验证适配器。", fields: []Field{text("name", "渠道名称", true, 100), selectField("provider", "服务商", "alipay", "wxpay"), {Name: "form_schema", Label: "资料表单 JSON", Type: "json", Required: true, MaxLength: 20000}, flag("enabled", "启用资料", false)}}
	definitions["onboarding-applications"] = definition{title: "进件商户资料", tenant: true, description: "保存本地进件草稿和人工审核状态；不代表已获得上游商户资格。", fields: []Field{administrator(text("merchant_id", "平台商户 ID", false, 19)), text("business_name", "经营名称", true, 200), text("contact_name", "联系人", true, 100), text("contact_phone", "联系电话", true, 40), text("channel_id", "进件渠道 ID", true, 19), administrator(selectField("status", "本地审核状态", "draft", "under_review", "action_required", "approved", "rejected")), text("remark", "备注", false, 2000)}}
	definitions["subchannels"] = definition{title: "子通道资料", tenant: true, description: "子商户绑定资料；实际交易能力由支付适配器验证。", fields: []Field{administrator(text("merchant_id", "平台商户 ID", false, 19)), text("name", "名称", true, 100), text("channel_id", "通道 ID", true, 19), text("provider_merchant_id", "上游子商户号", true, 100), administrator(flag("approved", "审核通过", false)), flag("enabled", "启用资料", false)}}
	definitions["merchant-risk"] = definition{title: "商户管控记录", tenant: true, merchantReadOnly: true, description: "本地管控资料，渠道管控状态需通过独立同步核实。", fields: []Field{administrator(text("merchant_id", "商户 ID", false, 19)), administrator(selectField("status", "状态", "normal", "restricted", "blocked")), text("reason", "原因", true, 2000), text("reference", "来源单号", false, 128)}}
	definitions["complaints"] = definition{title: "交易投诉", tenant: true, description: "本地投诉工单及处理记录。退款和渠道回复分别执行，修改状态不会触发资金变更。", fields: []Field{administrator(text("merchant_id", "商户 ID", false, 19)), text("trade_no", "平台交易号", true, 64), text("subject", "主题", true, 200), {Name: "content", Label: "投诉内容", Type: "text", Required: true, MaxLength: 10000}, administrator(selectField("status", "处理状态", "open", "processing", "resolved", "closed")), selectField("pay_type", "支付方式", "alipay", "wxpay", "sandbox"), integer("amount", "投诉金额（分）", 0, 100000000000), text("reply", "本地处理回复", false, 10000)}}
	definitions["complaint-tasks"] = definition{title: "投诉同步任务配置", description: "只保存任务计划。未配置同步适配器时不会假报同步成功。", fields: []Field{text("name", "名称", true, 100), text("channel_id", "通道 ID", true, 19), integer("interval_seconds", "间隔（秒）", 60, 86400), flag("enabled", "启用计划资料", false)}}
	definitions["risk-records"] = definition{title: "风控记录", description: "人工记录及核实的风控事件。", fields: []Field{text("trade_no", "订单号", false, 64), selectField("decision", "决策", "allow", "review", "deny"), text("reason", "原因", true, 2000), text("source", "来源", true, 100)}}
	definitions["cron-tasks"] = definition{title: "计划任务配置", description: "持久任务计划资料。手动保存不等于任务已执行。", fields: []Field{text("name", "名称", true, 100), selectField("kind", "任务类型", "payment.notify", "refund.reconcile", "transfer.reconcile"), integer("interval_seconds", "计划间隔（秒）", 60, 86400), flag("enabled", "启用计划资料", false)}}
	definitions["settlement-settings"] = definition{title: "结算设置", tenant: true, description: "收款资料仅供人工核对；审批不会自动向银行付款。", fields: []Field{administrator(text("merchant_id", "商户 ID", false, 19)), selectField("account_type", "收款类型", "bank", "alipay", "wxpay"), text("account_name", "收款姓名", true, 100), text("account_reference", "收款账户", true, 256), flag("enabled", "启用", true)}}
	definitions["collection-codes"] = definition{title: "聚合收款", tenant: true, description: "收款码展示资料，不在此创建真实支付订单。", fields: []Field{administrator(text("merchant_id", "商户 ID", false, 19)), text("name", "名称", true, 100), text("style_id", "样式 ID", true, 19), text("title", "收款标题", true, 100), flag("enabled", "启用", true)}}
	definitions["plugin-configs"] = definition{title: "本地插件配置", description: "仅保存扩展资料。PHP 插件不可载入 Go 进程，本资源不会安装或执行上传的代码。", fields: []Field{text("name", "扩展名称", true, 100), text("version", "版本", true, 64), {Name: "configuration", Label: "公开配置 JSON", Type: "json", MaxLength: 10000}, flag("enabled", "启用配置资料", false)}}
	definitions["template-configs"] = definition{title: "历史模板资料", description: "旧通用模板资料仅供只读核查；实际渲染使用模板页面的类型化配置与版本。", fields: []Field{text("name", "模板名称", true, 100), text("title", "展示标题", true, 100), text("accent_color", "主题颜色", false, 7), flag("enabled", "启用资料", false)}}
	definitions["addon-configs"] = definition{title: "扩展配置", description: "本地扩展参数，不自动调用外部服务或执行脚本。", fields: []Field{text("name", "名称", true, 100), {Name: "configuration", Label: "公开配置 JSON", Type: "json", MaxLength: 10000}, flag("enabled", "启用资料", false)}}
	// These status fields have safe server defaults for merchant-created drafts.
	for resource, fieldName := range map[string]string{"onboarding-applications": "status", "complaints": "status", "merchant-risk": "status"} {
		d := definitions[resource]
		for i := range d.fields {
			if d.fields[i].Name == fieldName {
				switch resource {
				case "onboarding-applications":
					d.fields[i].Default = "draft"
				case "complaints":
					d.fields[i].Default = "open"
				case "merchant-risk":
					d.fields[i].Default = "normal"
				}
			}
		}
		definitions[resource] = d
	}
}
