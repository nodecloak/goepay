package backoffice

import (
	"strconv"
	"strings"
)

// Original admin/set.php 商户相关 feature switches and 交易投诉/进件 toggles,
// plus 注册付费. Each is a plain 0/1 switch (or, for reg_pay_uid/_price, an id
// and a yuan amount) the original console shows on the 商户相关 / 交易投诉 pages:
//
//	internal_transfer "0"/"1"  开启站内转账     (merchant 站内转账 menu+endpoint)
//	recharge          "0"/"1"  开启余额充值     (merchant 余额充值 menu+endpoint)
//	onecode           "0"/"1"  开启聚合收款码   (merchant 聚合收款 menu+endpoint)
//	complain_open     "0"/"1"  开启交易投诉     (merchant 交易投诉 menu+endpoint)
//	applyments_open   "0"/"1"  开启商户进件     (merchant 商户进件 menu+endpoint)
//	mchrisk_open      "0"/"1"  开启商户管控记录查看
//	reg_pay           "0"/"1"  注册付费开关
//	reg_pay_price     yuan     注册付费金额
//	reg_pay_uid       id       注册/充值/购买会员收款商户ID
//
// The feature switches are read by MerchantFeatureFlags and enforced at the
// merchant console (menu hidden) and the matching workflow endpoints (refused).
func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "开启站内转账", Value: "internal_transfer"},
				Option{Label: "开启余额充值", Value: "recharge"},
				Option{Label: "开启聚合收款码", Value: "onecode"},
				Option{Label: "开启交易投诉", Value: "complain_open"},
				Option{Label: "开启商户进件功能", Value: "applyments_open"},
				Option{Label: "开启商户管控记录查看", Value: "mchrisk_open"},
				Option{Label: "注册付费", Value: "reg_pay"},
				Option{Label: "注册付费金额", Value: "reg_pay_price"},
				Option{Label: "注册/充值/购买会员收款商户ID", Value: "reg_pay_uid"},
			)
		}
	}
	definitions["settings"] = d
}

// MerchantFeatureKeys are the plain 0/1 merchant feature switches the merchant
// console and its workflow endpoints gate on.
var MerchantFeatureKeys = []string{"internal_transfer", "recharge", "onecode", "complain_open", "applyments_open", "mchrisk_open"}

// validateMerchantFeatureSetting reports whether key is a merchant feature or
// registration setting and, if so, whether value is legal (matching the
// original: switches 0/1, reg_pay_price a non-negative yuan amount, reg_pay_uid
// a positive merchant id or empty).
func validateMerchantFeatureSetting(key, value string) (bool, error) {
	switch key {
	case "internal_transfer", "recharge", "onecode", "complain_open", "applyments_open", "mchrisk_open", "reg_pay":
		if value != "0" && value != "1" {
			return true, ErrInvalid
		}
		return true, nil
	case "reg_pay_uid":
		if value == "" {
			return true, nil
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != value {
			return true, ErrInvalid
		}
		return true, nil
	case "reg_pay_price":
		if value == "" {
			return true, nil
		}
		// yuan with at most two decimals, non-negative, matching settingStorage(money).
		if !strings.ContainsAny(value, "0123456789") {
			return true, ErrInvalid
		}
		dot := strings.IndexByte(value, '.')
		whole, frac := value, ""
		if dot >= 0 {
			whole, frac = value[:dot], value[dot+1:]
		}
		if whole == "" || len(frac) > 2 {
			return true, ErrInvalid
		}
		for _, part := range []string{whole, frac} {
			for _, r := range part {
				if r < '0' || r > '9' {
					return true, ErrInvalid
				}
			}
		}
		return true, nil
	}
	return false, nil
}
