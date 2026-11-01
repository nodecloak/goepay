package backoffice

import (
	"goepay/internal/merchantpolicy"
	"goepay/internal/ordersubject"
)

func init() {
	d := definitions["groups"]
	domain := selectField("pay_domain_forbid", "未授权支付域名禁止支付", "", "_", "0", "1")
	domain.Required = false
	domain.Options = []Option{{Label: "缺省（与系统设置一致）", Value: ""}, {Label: "缺省（保留原值）", Value: "_"}, {Label: "关闭", Value: "0"}, {Label: "开启", Value: "1"}}
	domain.Description = "仅限制新支付意图的通知与返回域名；开启后两者都须属于商户已审核且启用的授权域名。原参数一致的订单重放保持原意图。"
	name := text("ordername", "商品名称自定义", false, 1024)
	name.PreserveWhitespace = true
	name.Description = "留空继承全局商品名称；商户、通道配置优先。支持[name]、[order]、[outorder]、[qq]、[phone]、[time]，以|分隔候选名称。非空的0和_按字面名称处理；仅影响首次发送，不改变订单原商品名。"
	d.fields = append(d.fields, domain, name)
	definitions["groups"] = d
}

func validateGroupPaymentSettings(values map[string]any) error {
	if raw, exists := values["pay_domain_forbid"]; exists {
		value, ok := raw.(string)
		if !ok {
			return ErrInvalid
		}
		if _, err := merchantpolicy.ParsePaymentDomainOverride(value); err != nil {
			return ErrInvalid
		}
	}
	if raw, exists := values["ordername"]; exists {
		value, ok := raw.(string)
		if !ok || (ordersubject.Policy{Scope: "global", Template: value}).Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}
