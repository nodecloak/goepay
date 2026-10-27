package backoffice

import "encoding/json"

func protectedPlatformSetting179(key string) bool {
	return key == "close_keylogin" || key == "user_refund" || key == "__platform_merchant_access179" || key == "scanpay_open" || key == "mail_interval_email" || key == "mail_interval_ip" || key == "mail_daily_email" || key == "mail_daily_ip"
}

// These settings are edited by the original-super typed CAS endpoint. Generic
// writes cannot rename another setting into a gate or remove its private anchor.
func rejectPlatformSetting179(in map[string]json.RawMessage, current map[string]any) error {
	key, _ := current["key"].(string)
	if protectedPlatformSetting179(key) {
		return ErrForbidden
	}
	if raw, present := in["key"]; present {
		if json.Unmarshal(raw, &key) == nil && protectedPlatformSetting179(key) {
			return ErrForbidden
		}
	}
	return nil
}
