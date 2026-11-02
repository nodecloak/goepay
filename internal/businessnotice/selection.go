package businessnotice

import (
	"errors"
	"goepay/internal/merchantnotice"
)

var ErrInvalid = errors.New("invalid administrator business notice")
var ErrConflict = errors.New("administrator business notice proof conflict")
var ErrState = errors.New("administrator business notice source unavailable")
var ErrNotFound = errors.New("administrator business notice not found")

// UID0 business scenes use another switch namespace than sendAdminMessage.
func SettingKey(scene string) (string, error) {
	switch scene {
	case "regaudit", "apply", "domain", "complain_all", "channellimit":
		return "msgconfig_" + scene, nil
	case "internal_transfer":
		return "msgconfig_domain", nil
	default:
		return "", ErrInvalid
	}
}
func ModeFor(scene, value string) (merchantnotice.Transport, error) {
	if _, err := SettingKey(scene); err != nil {
		return "", err
	}
	switch value {
	case "0":
		return "", nil
	case "1":
		return merchantnotice.SMTP, nil
	case "2":
		return merchantnotice.Robot, nil
	case "3":
		if scene == "complain_all" {
			return merchantnotice.SMS, nil
		}
		return "", ErrInvalid
	case "4":
		return merchantnotice.Telegram, nil
	case "5":
		return merchantnotice.QQBot, nil
	default:
		return "", ErrInvalid
	}
}
