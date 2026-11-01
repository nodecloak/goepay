package backoffice

import (
	"encoding/json"

	"goepay/internal/merchantpolicy"
)

func init() {
	d := definitions["groups"]
	flag := selectField("wxcombine_open", "微信合单支付", "", "_", "0", "1")
	flag.Required = false
	flag.Options = []Option{{Label: "缺省（与系统设置一致）", Value: ""}, {Label: "缺省（与系统设置一致）", Value: "_"}, {Label: "关闭", Value: "0"}, {Label: "开启", Value: "1"}}
	minimum := text("wxcombine_minmoney", "合单支付最小金额（元）", false, 32)
	minimum.Pattern = `^(?:_|[0-9]+(?:\.[0-9]{1,2})?)?$`
	minimum.Description = "留空或_继承系统，0不启用合单；仅影响首次支付发送。"
	capacity := text("wxcombine_submoney", "合单支付子单最大金额（元）", false, 32)
	capacity.Pattern = minimum.Pattern
	capacity.Description = "留空或_继承系统；沿用现有3至50子单拆分规则，修改不会重拆原订单。"
	d.fields = append(d.fields, flag, minimum, capacity)
	definitions["groups"] = d
}

func validateGroupCombineSettings(values map[string]any) error {
	raw, err := json.Marshal(values)
	if err != nil {
		return ErrInvalid
	}
	if _, err = merchantpolicy.ResolveCombinePolicy(nil, raw); err != nil {
		return ErrInvalid
	}
	return nil
}
