package backoffice

import "goepay/internal/grouppolicy"

func init() {
	d := definitions["groups"]
	fee := text("transfer_rate", "代付手续费（%）", false, 6)
	fee.Description = "留空或_沿用商户原代付费率；填0使用有效结算比例，填0.00明确免代付手续费；最多两位小数，范围0至100%。"
	fee.Pattern = `^(?:_|[0-9]+(?:\.[0-9]{1,2})?)?$`
	red := selectField("user_transfer_red", "创建红包功能", "", "_", "0", "1")
	red.Required = false
	red.Options = []Option{{Label: "缺省（与系统设置一致）", Value: ""}, {Label: "缺省（保留原值）", Value: "_"}, {Label: "关闭", Value: "0"}, {Label: "开启", Value: "1"}}
	red.Description = "仅限制新红包创建，仍需普通代付权限；已创建红包按原流程领取与核对，不会重复发送。"
	d.fields = append(d.fields, fee, red)
	definitions["groups"] = d
}

func validateGroupTransferSettings(values map[string]any) error {
	if raw, exists := values["transfer_rate"]; exists {
		value, ok := raw.(string)
		if !ok {
			return ErrInvalid
		}
		if _, err := grouppolicy.ParseTransferFeeOverride(value); err != nil {
			return ErrInvalid
		}
	}
	if raw, exists := values["user_transfer_red"]; exists {
		value, ok := raw.(string)
		if !ok {
			return ErrInvalid
		}
		if _, err := grouppolicy.ParseRedpackFeatureOverride(value); err != nil {
			return ErrInvalid
		}
	}
	return nil
}
