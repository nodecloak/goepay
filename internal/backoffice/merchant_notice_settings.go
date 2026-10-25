package backoffice

import (
	"encoding/json"
	"goepay/internal/businessnotice"
	notice "goepay/internal/merchantnotice"
	"strings"
)

func validateMerchantNoticeRaw(in map[string]json.RawMessage, current map[string]any) error {
	key, _ := current["key"].(string)
	value, _ := current["value"].(string)
	if raw, ok := in["key"]; ok {
		if json.Unmarshal(raw, &key) != nil {
			return ErrInvalid
		}
	}
	if raw, ok := in["value"]; ok {
		if json.Unmarshal(raw, &value) != nil {
			return ErrInvalid
		}
	}
	canonical := strings.TrimSpace(key)
	if known, err := notice.ValidatePlatformSetting(canonical, value); known && (err != nil || key != canonical) {
		return ErrInvalid
	}
	if known, err := businessnotice.ValidatePlatformSetting(canonical, value); known && (err != nil || key != canonical) {
		return ErrInvalid
	}
	return nil
}

func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			for _, setting := range notice.PlatformSettings() {
				d.fields[i].Options = append(d.fields[i].Options, Option{Label: setting.Label, Value: setting.Key})
			}
		}
	}
	d.description += " 消息提醒开关沿用原0/1，微信模板ID和字段映射可在此配置。保存不发送消息；实际外发由独立worker开关控制。"
	definitions["settings"] = d
	d = definitions["groups"]
	field := selectField("user_transfer", "代付功能", "", "0", "_", "1")
	field.Required = false
	field.Default = ""
	field.Options = []Option{{Label: "缺省（与系统设置一致）", Value: ""}, {Label: "缺省（旧值0）", Value: "0"}, {Label: "缺省（旧值_）", Value: "_"}, {Label: "开启", Value: "1"}}
	d.fields = append(d.fields, field)
	definitions["groups"] = d
}
