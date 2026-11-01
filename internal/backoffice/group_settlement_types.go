package backoffice

import (
	"encoding/json"
	"goepay/internal/grouppolicy"
)

func init() {
	d := definitions["groups"]
	labels := map[string]string{"alipay": "支付宝结算", "wxpay": "微信结算", "bank": "银行卡结算"}
	for _, typ := range grouppolicy.SettlementRecipientTypes() {
		key, _ := grouppolicy.SettlementTypeKey(typ)
		field := selectField(key, labels[typ], "", "_", "0", "1")
		field.Required = false
		field.Default = "_"
		field.Options = []Option{{Label: "缺省（与系统设置一致）", Value: ""}, {Label: "缺省（与系统设置一致）", Value: "_"}, {Label: "关闭", Value: "0"}, {Label: "开启", Value: "1"}}
		field.Description = "仅限制新收款资料与新结算申请；开启仍受系统方式及实际出款通道能力约束。"
		d.fields = append(d.fields, field)
	}
	definitions["groups"] = d
}

func validateGroupSettlementTypes(values map[string]any) error {
	for _, typ := range grouppolicy.SettlementRecipientTypes() {
		key, _ := grouppolicy.SettlementTypeKey(typ)
		value, exists := values[key]
		if !exists {
			continue
		}
		text, ok := value.(string)
		if !ok {
			return ErrInvalid
		}
		raw, _ := json.Marshal(text)
		if _, err := grouppolicy.ParseSettlementTypeSwitch(raw); err != nil {
			return ErrInvalid
		}
	}
	return nil
}
