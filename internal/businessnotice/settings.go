package businessnotice

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantnotice"
	"net/mail"
	"regexp"
	"strconv"
)

type SettingsReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

var settingKeys = []string{"msgconfig_regaudit", "msgconfig_apply", "msgconfig_domain", "mail_recv", "mail_name", "msgtelegram_chatid", "qqbot_openid", "sitename", "site_name"}
var telegramName = regexp.MustCompile(`^@[A-Za-z0-9_]{1,64}$`)
var botID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func PlatformSettings() []merchantnotice.PlatformSetting {
	return []merchantnotice.PlatformSetting{{Key: "msgconfig_regaudit", Label: "新商户待审核提醒（0关闭/1邮件/2机器人/4Telegram/5QQ）"}, {Key: "msgconfig_apply", Label: "手动提现申请提醒（0关闭/1邮件/2机器人/4Telegram/5QQ）"}, {Key: "msgconfig_domain", Label: "域名与站内转账待审核提醒（0关闭/1邮件/2机器人/4Telegram/5QQ）"}}
}
func ValidatePlatformSetting(key, value string) (bool, error) {
	for _, scene := range []string{"regaudit", "apply", "domain"} {
		if key == "msgconfig_"+scene {
			_, err := ModeFor(scene, value)
			return true, err
		}
	}
	return false, nil
}
func validateSetting(key, value string) error {
	if known, err := ValidatePlatformSetting(key, value); known {
		return err
	}
	switch key {
	case "sitename", "site_name":
		_, err := merchantnotice.ValidatePlatformSetting(key, value)
		return err
	case "mail_recv", "mail_name":
		if value == "" {
			return nil
		}
		a, err := mail.ParseAddress(value)
		if err != nil || a.Name != "" || a.Address != value || len(value) > 254 {
			return ErrInvalid
		}
		for _, c := range value {
			if c < 33 || c > 126 {
				return ErrInvalid
			}
		}
		return nil
	case "msgtelegram_chatid":
		if value == "" {
			return nil
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if (err != nil || n == 0 || strconv.FormatInt(n, 10) != value) && !telegramName.MatchString(value) {
			return ErrInvalid
		}
		return nil
	case "qqbot_openid":
		if value != "" && !botID.MatchString(value) {
			return ErrInvalid
		}
		return nil
	default:
		return ErrInvalid
	}
}

// One database snapshot of original nonsecret selectors and destinations. When
// used in a caller transaction, shared row locks protect the first claim's
// mode decision until commit. No URL/token/AppSecret is read here.
func ReadSettings(ctx context.Context, q SettingsReader) (map[string]string, error) {
	if ctx == nil || ctx.Err() != nil || q == nil {
		return nil, ErrInvalid
	}
	rows, err := q.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1::text[]) ORDER BY id FOR SHARE`, settingKeys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, value string
		var raw []byte
		if err = rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		if _, duplicate := out[key]; duplicate || len(raw) < 2 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil || validateSetting(key, value) != nil {
			return nil, ErrInvalid
		}
		out[key] = value
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if _, present := out["sitename"]; !present {
		if alias, ok := out["site_name"]; ok {
			out["sitename"] = alias
		} else {
			out["sitename"] = "易支付"
		}
	}
	return out, nil
}
