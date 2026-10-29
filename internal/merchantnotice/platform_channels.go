package merchantnotice

import (
	"context"
	"encoding/json"
)

// PlatformNoticeSwitches is the set of read-only platform facts the merchant
// 消息推送 matrix needs to decide which event×channel cells can deliver right
// now. They are the exact switches globalSwitches() reads for real delivery,
// hoisted across all scenes so the console can render Pro's full grid without
// guessing: mail/robot is enabled per scene (msgconfig_<scene>), while
// Telegram/QQ Bot/Voice are global toggles. GroupSMS is always on (the original
// 会员到期 forced-SMS route). This never reads a credential or a merchant row.
type PlatformNoticeSwitches struct {
	// MailRobot[scene] reports the per-scene 邮件/群机器人 platform switch.
	MailRobot map[string]bool `json:"mail_robot"`
	Telegram  bool            `json:"telegram"`
	QQBot     bool            `json:"qqbot"`
	Voice     bool            `json:"voice"`
	GroupSMS  bool            `json:"group_sms"`
}

var noticeScenes80 = []string{"order", "settle", "login", "complain", "mchrisk", "balance", "group", "domain_audit", "daily_report"}

// PlatformNoticeSwitches reads the platform notice switches without locking any
// merchant or profile row. Read-only; it never mutates a ledger or settings row.
func (s *Store) PlatformNoticeSwitches(ctx context.Context) (PlatformNoticeSwitches, error) {
	out := PlatformNoticeSwitches{MailRobot: map[string]bool{}, GroupSMS: true}
	if !s.available() || ctx == nil {
		return out, ErrState
	}
	keys := make([]string, 0, len(noticeScenes80)+3)
	for _, scene := range noticeScenes80 {
		keys = append(keys, "msgconfig_"+scene)
	}
	keys = append(keys, "telegramnotice", "qqbotnotice", "voicenotice")
	rows, err := s.pool.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1::text[]) ORDER BY id`, keys)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	values := map[string]bool{}
	for rows.Next() {
		var key, value string
		var raw []byte
		if rows.Scan(&key, &raw) != nil || json.Unmarshal(raw, &value) != nil || value != "0" && value != "1" {
			return PlatformNoticeSwitches{}, ErrState
		}
		if _, exists := values[key]; exists {
			return PlatformNoticeSwitches{}, ErrConflict
		}
		values[key] = value == "1"
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	for _, scene := range noticeScenes80 {
		out.MailRobot[scene] = values["msgconfig_"+scene]
	}
	out.Telegram = values["telegramnotice"]
	out.QQBot = values["qqbotnotice"]
	out.Voice = values["voicenotice"]
	return out, nil
}
