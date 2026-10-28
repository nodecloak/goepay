package backoffice

import "goepay/internal/ledger"

// Original admin/set.php 支付宝/微信支付合单支付配置 (WeChat v3 only).
func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "微信开启合单支付", Value: "wxcombine_open"},
				Option{Label: "合单支付最小金额", Value: "wxcombine_minmoney"},
				Option{Label: "合单支付子单最大金额", Value: "wxcombine_submoney"},
			)
		}
	}
	definitions["settings"] = d
}

// validateCombineSetting207 reports whether key is a combine setting and,
// if so, whether value is legal: the switch is 0/1, amounts are empty or 0
// (rule off) or a positive yuan amount with at most two decimals.
func validateCombineSetting207(key, value string) (bool, error) {
	switch key {
	case "wxcombine_open":
		if value != "0" && value != "1" {
			return true, ErrInvalid
		}
		return true, nil
	case "wxcombine_minmoney", "wxcombine_submoney":
		if value == "" {
			return true, nil
		}
		if v, err := ledger.Parse(value); err != nil || v < 0 {
			return true, ErrInvalid
		}
		return true, nil
	}
	return false, nil
}
