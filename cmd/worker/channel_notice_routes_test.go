package main

import (
	"goepay/internal/payment"
	"strings"
	"testing"
)

func TestChannelNotice77OriginalRiskSelectorUsesOneTransportOrOriginalMailFallback(t *testing.T) {
	for _, tc := range []struct{ risk, targetKey, target, mode string }{
		{"1", "GOEPAY_ADMIN_MSGROBOT_URL", "https://oapi.dingtalk.com/robot/send?access_token=synthetic77", "robot"},
		{"2", "GOEPAY_ADMIN_MSGTELEGRAM_CHATID", "-12345", "telegram"},
		{"3", "GOEPAY_ADMIN_QQBOT_OPENID", "local_openid77", "qqbot"},
	} {
		m := noticeEnvironment76()
		m["GOEPAY_ADMIN_MSGCONFIG_RISK"] = tc.risk
		m[tc.targetKey] = tc.target
		m["GOEPAY_ADMIN_LOGIN_TELEGRAM_TOKEN"] = "12345:synthetic77"
		m["GOEPAY_ADMIN_QQBOT_APPID"] = "local_app77"
		m["GOEPAY_ADMIN_QQBOT_APPSECRET"] = "synthetic-secret77"
		m["GOEPAY_SMTP_HOST"] = "not-used://"
		s, to, e := configuredChannelNotices(func(k string) string { return m[k] })
		if e != nil || s == nil || to != tc.target {
			t.Fatal("original selected branch unavailable", tc.mode, e)
		}
		r, ok := s.(payment.ChannelNoticeRoute)
		if !ok || r.ChannelNoticeMode() != tc.mode {
			t.Fatal("wrong selected route", tc.mode)
		}
		delete(m, tc.targetKey)
		m["GOEPAY_SMTP_HOST"] = "smtp.example.invalid"
		m["GOEPAY_ADMIN_MAIL_RECV"] = "receive@example.test"
		m["GOEPAY_ADMIN_MAIL_NAME"] = "fallback@example.test"
		s, to, e = configuredChannelNotices(func(k string) string { return m[k] })
		if e != nil || to != "receive@example.test" || s.(payment.ChannelNoticeRoute).ChannelNoticeMode() != "smtp" {
			t.Fatal("missing target did not use original mail_recv fallback", e)
		}
		delete(m, "GOEPAY_ADMIN_MAIL_RECV")
		s, to, e = configuredChannelNotices(func(k string) string { return m[k] })
		if e != nil || s == nil || to != "fallback@example.test" {
			t.Fatal("mail_name fallback unavailable", e)
		}
	}
}

func TestChannelNotice77SelectedInvalidBranchNeverFallsBackToWorkingMail(t *testing.T) {
	for _, changes := range []map[string]string{
		{"GOEPAY_ADMIN_MSGCONFIG_RISK": "01"},
		{"GOEPAY_ADMIN_MSGCONFIG_RISK": "4"},
		{"GOEPAY_ADMIN_MSGCONFIG_RISK": "1", "GOEPAY_ADMIN_MSGROBOT_URL": "https://oapi.dingtalk.com.evil.invalid/robot/send?access_token=synthetic-secret77"},
		{"GOEPAY_ADMIN_MSGCONFIG_RISK": "2", "GOEPAY_ADMIN_MSGTELEGRAM_CHATID": "-12345"},
		{"GOEPAY_ADMIN_MSGCONFIG_RISK": "2", "GOEPAY_ADMIN_MSGTELEGRAM_CHATID": "0123", "GOEPAY_ADMIN_LOGIN_TELEGRAM_TOKEN": "12345:synthetic77"},
		{"GOEPAY_ADMIN_MSGCONFIG_RISK": "3", "GOEPAY_ADMIN_QQBOT_OPENID": "local77", "GOEPAY_ADMIN_QQBOT_APPID": "app77"},
	} {
		m := noticeEnvironment76()
		for k, v := range changes {
			m[k] = v
		}
		s, to, e := configuredChannelNotices(func(k string) string { return m[k] })
		if e == nil || s != nil || to != "" || strings.Contains(e.Error(), "synthetic-secret77") {
			t.Fatal("invalid selected profile silently sent mail or leaked", changes["GOEPAY_ADMIN_MSGCONFIG_RISK"])
		}
	}
}
