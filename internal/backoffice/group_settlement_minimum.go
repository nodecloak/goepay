package backoffice

import "goepay/internal/grouppolicy"

func init() {
	d := definitions["groups"]
	minimum := text("settle_minmoney", "最低结算金额（元）", false, 32)
	minimum.Pattern = `^(?:_|[0-9]+(?:\.[0-9]{1,2})?)?$`
	minimum.Description = "留空或_继承系统最低结算金额，0不限制；仅影响新手动申请与普通定时结算。"
	d.fields = append(d.fields, minimum)
	definitions["groups"] = d
}

func validateGroupSettlementMinimum(values map[string]any) error {
	raw, exists := values["settle_minmoney"]
	if !exists {
		return nil
	}
	value, ok := raw.(string)
	if !ok {
		return ErrInvalid
	}
	if _, err := grouppolicy.ParseSettlementMinimum(value); err != nil {
		return ErrInvalid
	}
	return nil
}
