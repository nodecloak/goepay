package main

import (
	"context"
	"goepay/internal/businessnotice"
	"goepay/internal/merchantnotice"
	"testing"
)

func TestAdminBusiness83RuntimeOffAndInvalidFlagsReadNoCredentials(t *testing.T) {
	for _, flag := range []string{"", "false", "TRUE", "1"} {
		reads := 0
		r, err := newBusinessNoticeRuntime(nil, func(key string) string {
			if key == "GOEPAY_ENABLE_ADMIN_BUSINESS_NOTICES" {
				return flag
			}
			reads++
			return "must-not-read"
		})
		if r != nil || reads != 0 || ((flag == "" || flag == "false") != (err == nil)) {
			t.Fatal(flag, reads, err)
		}
	}
}
func TestAdminBusiness83RuntimeUsesSelectedAccountWithoutRiskFallback(t *testing.T) {
	env := map[string]string{"GOEPAY_SMTP_HOST": "smtp.example.test", "GOEPAY_SMTP_PORT": "465", "GOEPAY_SMTP_SECURITY": "implicit", "GOEPAY_SMTP_USERNAME": "synthetic83", "GOEPAY_SMTP_PASSWORD": "synthetic-password83", "GOEPAY_SMTP_FROM": "sender@example.test", "GOEPAY_ADMIN_MAIL_RECV": "fallback83@example.test", "GOEPAY_ADMIN_MSGROBOT_URL": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic83", "GOEPAY_ADMIN_LOGIN_TELEGRAM_TOKEN": "83001:synthetic83", "GOEPAY_ADMIN_QQBOT_APPID": "83001", "GOEPAY_ADMIN_QQBOT_APPSECRET": "synthetic-qq83", "GOEPAY_ADMIN_QQBOT_OPENID": "synthetic-openid83"}
	r := &businessNoticeRuntime{getenv: func(key string) string {
		if key == "GOEPAY_ADMIN_MSGCONFIG_RISK" {
			t.Fatal("read risk namespace")
		}
		return env[key]
	}}
	for _, kind := range []merchantnotice.Transport{merchantnotice.SMTP, merchantnotice.Robot, merchantnotice.Telegram, merchantnotice.QQBot} {
		route, err := r.ResolveBusinessNotice(context.Background(), kind, businessnotice.Account{Settings: map[string]string{"msgtelegram_chatid": "-1000000000083"}})
		if err != nil || route.Sender == nil || route.Sender.NoticeTransport() != kind || route.Sender.ValidateNoticeRecipient(route.Recipient) != nil {
			t.Fatal(kind, err)
		}
		if kind == merchantnotice.QQBot {
			expected, e := r.BusinessNoticeAccountID(context.Background(), kind)
			if e != nil || expected != "83001" || route.Sender.(merchantnotice.AccountBoundSender).NoticeAccountID() != expected {
				t.Fatal("unbound QQ account")
			}
		}
	}
	route, err := r.ResolveBusinessNotice(context.Background(), merchantnotice.SMTP, businessnotice.Account{Settings: map[string]string{"mail_recv": ""}})
	if err == nil || route.Sender != nil {
		t.Fatal("explicit empty target adopted process fallback")
	}
	delete(env, "GOEPAY_ADMIN_MSGROBOT_URL")
	route, err = r.ResolveBusinessNotice(context.Background(), merchantnotice.Robot, businessnotice.Account{})
	if err == nil || route.Sender != nil {
		t.Fatal("unconfigured robot fell back to mail")
	}
}
