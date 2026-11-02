package backoffice

import (
	"encoding/json"
	"goepay/internal/merchantnotice"
	"strings"
)

// Only keys whose real consumer defines an empty/default value may be cleared.
// In particular a required switch or a WeChat template ID remains required.
func optionalSettingDefinition228(d definition, raw map[string]json.RawMessage, current map[string]any) definition {
	if !d.layoutSettings {
		return d
	}
	key, _ := current["key"].(string)
	if v, ok := raw["key"]; ok && json.Unmarshal(v, &key) != nil {
		return d
	}
	optional := false
	switch key {
	case "blockname", "blockalert", "blackmsg", "pay_force_cashier_pids", "user_deposit_min", "user_deposit_day", "mail_recv", "mail_name", "msgtelegram_chatid", "qqbot_openid":
		optional = true
	}
	if strings.HasPrefix(key, "wxnotice_tpl_") {
		known, err := merchantnotice.ValidatePlatformSetting(key, "")
		optional = known && err == nil
	}
	if !optional {
		return d
	}
	d.fields = append([]Field(nil), d.fields...)
	for i := range d.fields {
		if d.fields[i].Name == "value" {
			d.fields[i].Required = false
		}
	}
	return d
}
