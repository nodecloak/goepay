package merchantnotice

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"strings"
)

func noticeSiteName(ctx context.Context, tx pgx.Tx) (string, error) {
	rows, err := tx.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key' IN('sitename','site_name') ORDER BY id`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var key, value string
		var raw []byte
		if err = rows.Scan(&key, &raw); err != nil {
			return "", err
		}
		if _, exists := values[key]; exists {
			return "", ErrConflict
		}
		if json.Unmarshal(raw, &value) != nil || !safeValue(value, 128, false) {
			return "", ErrState
		}
		values[key] = value
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if value, ok := values["sitename"]; ok {
		return value, nil
	}
	if value, ok := values["site_name"]; ok {
		return value, nil
	}
	return "易支付", nil
}

type PlatformSetting struct{ Key, Label string }

var wechatSettingFields = []struct{ scene, fields string }{
	{"order", "no name money time outno"},
	{"settle", "type account money realmoney time"},
	{"login", "user time name ip iploc"},
	{"complain", "order_no time reason type name"},
	{"balance", "user time money msg"},
	{"wxtransfer", "money time"},
}

// Only original nonsecret switches and field mappings belong in generic
// platform settings. Tokens, secrets, webhook URLs and targets do not.
func PlatformSettings() []PlatformSetting {
	out := make([]PlatformSetting, 0, 48)
	for _, scene := range []struct{ key, label string }{{"order", "订单支付成功"}, {"settle", "结算完成"}, {"login", "账号登录"}, {"complain", "交易投诉"}, {"mchrisk", "渠道商户违规处置"}, {"balance", "余额不足"}, {"group", "会员到期"}, {"domain_audit", "授权域名审核"}, {"daily_report", "每日交易统计"}} {
		key := "msgconfig_" + scene.key
		out = append(out, PlatformSetting{key, scene.label + " 邮件/机器人开关（0/1） " + key})
	}
	for _, pair := range []PlatformSetting{{"telegramnotice", "Telegram 提醒开关（0/1）"}, {"qqbotnotice", "QQ Bot 提醒开关（0/1）"}, {"voicenotice", "语音提醒开关（0/1）"}, {"user_transfer", "原用户代付开关（0/1）"}, {"sitename", "原网站名称"}} {
		out = append(out, pair)
	}
	for _, pair := range []PlatformSetting{{"wxnotice", "公众号消息"}, {"mailnotice", "邮件消息"}, {"robotnotice", "群机器人Webhook"}, {"smsnotice", "短信消息"}} {
		out = append(out, pair)
	}
	for _, scene := range wechatSettingFields {
		base := "wxnotice_tpl_" + scene.scene
		out = append(out, PlatformSetting{base, "微信模板 ID " + base})
		for _, field := range strings.Fields(scene.fields) {
			key := base + "_" + field
			out = append(out, PlatformSetting{key, "微信模板字段 " + key})
		}
	}
	return out
}

func ValidatePlatformSetting(key, value string) (bool, error) {
	if key == "sitename" || key == "site_name" {
		if !safeValue(value, 128, false) {
			return true, ErrInvalid
		}
		return true, nil
	}
	known := false
	for _, setting := range PlatformSettings() {
		if setting.Key == key {
			known = true
			break
		}
	}
	if !known {
		return false, nil
	}
	if strings.HasPrefix(key, "msgconfig_") || key == "telegramnotice" || key == "qqbotnotice" || key == "voicenotice" || key == "user_transfer" || key == "wxnotice" || key == "mailnotice" || key == "robotnotice" || key == "smsnotice" {
		if value != "0" && value != "1" {
			return true, ErrInvalid
		}
		return true, nil
	}
	for _, scene := range wechatSettingFields {
		base := "wxnotice_tpl_" + scene.scene
		if key == base {
			if !validTargetID(value) {
				return true, ErrInvalid
			}
			return true, nil
		}
		if strings.HasPrefix(key, base+"_") {
			if value != "" && !templateField80.MatchString(value) {
				return true, ErrInvalid
			}
			return true, nil
		}
	}
	return true, ErrInvalid
}
