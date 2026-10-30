package backoffice

import "strings"

// Original admin/set.php 商户相关 / 转账付款 switches that gate a whole merchant
// feature, plus the 验证方式 selector and the 聚合收款 / 收款域名 keys. Each is
// registered as a selectable settings key so the console surfaces it exactly as
// epay_pro does, and validated to the original shape.
//
//	user_totp_open    "0"/"1"  商户登录二次验证（TOTP）开关
//	subchannel_open   "0"/"1"  子商户支付通道开关
//	user_profitsharing"0"/"1"  用户分账功能开关
//	user_transfer_batch"0"/"1" 批量代付开关
//	verifytype        "0".."2" 商户账号验证方式（0不验证/1邮箱/2手机）
//	onecode_domains   text     聚合收款码可用域名（每行一个，最多 2KB）
//	pay_domain_open   "0"/"1"  收款页面域名校验开关
//	pay_domain_forbid text     收款页面禁止域名（每行一个，最多 2KB）
//
// The four feature switches default ON (a missing row leaves the feature
// available, preserving today's behaviour); only an explicit "0" hides the
// merchant entry and makes the matching endpoint refuse with the original
// merchant-facing wording. verifytype / onecode_domains / pay_domain_* are
// plain settings read by their business flows.
func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "商户登录二次验证开关", Value: "user_totp_open"},
				Option{Label: "子商户支付通道开关", Value: "subchannel_open"},
				Option{Label: "用户分账功能开关", Value: "user_profitsharing"},
				Option{Label: "批量代付开关", Value: "user_transfer_batch"},
				Option{Label: "商户账号验证方式", Value: "verifytype"},
				Option{Label: "聚合收款可用域名", Value: "onecode_domains"},
				Option{Label: "收款页面域名校验开关", Value: "pay_domain_open"},
				Option{Label: "收款页面禁止域名", Value: "pay_domain_forbid"},
			)
		}
	}
	definitions["settings"] = d
}

// MerchantSwitchDefaultOnKeys are the merchant feature switches added in the
// 商户相关/转账付款 parity pass that default ON: a missing row means available,
// an explicit "0" hides the merchant entry and refuses the endpoint. They share
// the MerchantFeatureVisible gate.
var MerchantSwitchDefaultOnKeys = []string{"user_totp_open", "subchannel_open", "user_profitsharing", "user_transfer_batch"}

// validateMerchantSwitchSetting reports whether key is one of the 商户相关/
// 转账付款 parity keys and, if so, whether value is legal: the feature switches
// and pay_domain_open are 0/1; verifytype is 0/1/2; the domain lists are newline
// separated text up to 2KB (empty allowed, matching an unset $conf value).
func validateMerchantSwitchSetting(key, value string) (bool, error) {
	switch key {
	case "user_totp_open", "subchannel_open", "user_profitsharing", "user_transfer_batch", "pay_domain_open":
		if value != "0" && value != "1" {
			return true, ErrInvalid
		}
		return true, nil
	case "verifytype":
		switch value {
		case "0", "1", "2":
			return true, nil
		}
		return true, ErrInvalid
	case "onecode_domains", "pay_domain_forbid":
		if len(value) > 2048 || strings.ContainsRune(value, '\x00') {
			return true, ErrInvalid
		}
		return true, nil
	}
	return false, nil
}
