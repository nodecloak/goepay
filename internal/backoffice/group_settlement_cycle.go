package backoffice

import "goepay/internal/grouppolicy"

func init() {
	d := definitions["groups"]
	cycle := selectField("settle_type", "结算周期", "", "_", "0", "1")
	cycle.Required = false
	cycle.Default = "_"
	cycle.Options = []Option{
		{Label: "缺省（与系统设置一致）", Value: ""},
		{Label: "缺省（与系统设置一致）", Value: "_"},
		{Label: "D+0（可结算全部余额）", Value: "0"},
		{Label: "D+1（可结算前1天的余额）", Value: "1"},
	}
	cycle.Description = "按有效用户组计算新出账的可用余额；修改不会重写原交易或创建自动出款授权。"
	d.fields = append(d.fields, cycle)
	definitions["groups"] = d
}

func validateGroupSettlementCycle(values map[string]any) error {
	raw, exists := values["settle_type"]
	if !exists {
		return nil
	}
	value, ok := raw.(string)
	if !ok {
		return ErrInvalid
	}
	if _, err := grouppolicy.ParseSettlementCycle(value); err != nil {
		return ErrInvalid
	}
	return nil
}
