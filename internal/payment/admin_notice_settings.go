package payment

import (
	"context"
	"encoding/json"
	"net/mail"
	"regexp"
	"strconv"
)

var adminNoticeSettingKeys = []string{"msgconfig_risk", "msgtelegram_chatid", "qqbot_openid", "mail_recv", "mail_name"}
var adminNoticeTelegramName = regexp.MustCompile(`^@[A-Za-z0-9_]{1,64}$`)
var adminNoticeQQIdentity = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func ValidateAdminNoticeSetting(key, value string) error {
	switch key {
	case "msgconfig_risk":
		if value != "0" && value != "1" && value != "2" && value != "3" {
			return ErrInvalid
		}
	case "msgtelegram_chatid":
		if value == "" {
			return nil
		}
		id, e := strconv.ParseInt(value, 10, 64)
		if (e != nil || id == 0 || strconv.FormatInt(id, 10) != value) && !adminNoticeTelegramName.MatchString(value) {
			return ErrInvalid
		}
	case "qqbot_openid":
		if value != "" && !adminNoticeQQIdentity.MatchString(value) {
			return ErrInvalid
		}
	case "mail_recv", "mail_name":
		if value == "" {
			return nil
		}
		address, e := mail.ParseAddress(value)
		if e != nil || address.Address != value || address.Name != "" || len(value) > 254 {
			return ErrInvalid
		}
		for _, r := range value {
			if r < 33 || r > 126 {
				return ErrInvalid
			}
		}
	default:
		return ErrInvalid
	}
	return nil
}

// This single MVCC statement returns only the original nonsecret selectors.
// A missing field permits the explicit process default; an empty target is a
// real saved value. Duplicates or malformed imports cannot choose an arbitrary
// recipient. Secret URLs/tokens are deliberately outside this projection.
func (s *Store) AdminChannelNoticeSettings(ctx context.Context) (map[string]string, error) {
	if ctx == nil || s == nil || s.pool == nil {
		return nil, ErrInvalid
	}
	rows, e := s.pool.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1::text[]) ORDER BY body->>'key',id`, adminNoticeSettingKeys)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, value string
		var raw []byte
		if e = rows.Scan(&key, &raw); e != nil {
			return nil, e
		}
		if _, duplicate := out[key]; duplicate || len(raw) < 2 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil || ValidateAdminNoticeSetting(key, value) != nil {
			return nil, ErrInvalid
		}
		out[key] = value
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	return out, nil
}
