package backoffice

import (
	"regexp"

	adapter "goepay/internal/adapters/payment"
)

// Original admin/set.php 微信V3转账产品类型 and the 商家转账（新） scene and
// report settings (transfer_wxpay_type / _scene_id / _info_type / _info_content).
func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "微信V3转账产品类型", Value: "transfer_wxpay_type"},
				Option{Label: "微信商家转账-场景ID", Value: "transfer_wxpay_scene_id"},
				Option{Label: "微信商家转账-报备信息类型", Value: "transfer_wxpay_info_type"},
				Option{Label: "微信商家转账-报备信息内容", Value: "transfer_wxpay_info_content"},
			)
		}
	}
	definitions["settings"] = d
}

var merchantTransferSceneSetting = regexp.MustCompile(`^[0-9]{1,8}$`)

// validateMerchantTransferSetting reports whether key is a 商家转账 setting
// and, if so, whether value is legal. Empty scene/report values are allowed
// (a bill is then refused with the original 未配置 wording); the report type
// and content lists must otherwise be the original '|'-separated shape.
func validateMerchantTransferSetting(key, value string) (bool, error) {
	switch key {
	case "transfer_wxpay_type":
		if value != "0" && value != "1" {
			return true, ErrInvalid
		}
	case "transfer_wxpay_scene_id":
		if value != "" && !merchantTransferSceneSetting.MatchString(value) {
			return true, ErrInvalid
		}
	case "transfer_wxpay_info_type", "transfer_wxpay_info_content":
		limit := 15
		if key == "transfer_wxpay_info_content" {
			limit = 32
		}
		if value != "" && !adapter.ValidTransferReportList(value, limit) {
			return true, ErrInvalid
		}
	default:
		return false, nil
	}
	return true, nil
}
