package main

import (
	"context"
	"testing"
	"time"

	notice "goepay/internal/merchantnotice"
)

func TestMerchantNotice80RuntimeDefaultOffDoesNotReadCredentialsOrRequireDatabase(t *testing.T) {
	for _, flag := range []string{"", "false", "TRUE", "1"} {
		reads := 0
		r, err := newMerchantNoticeRuntime(nil, func(key string) string {
			if key == "GOEPAY_ENABLE_MERCHANT_NOTICES" {
				return flag
			}
			reads++
			return "must-not-read"
		})
		if r != nil || reads != 0 || (flag == "" || flag == "false") != (err == nil) {
			t.Fatalf("default-off/invalid flag loaded private configuration: %q reads%d err%v", flag, reads, err)
		}
	}
}
func TestMerchantNotice80RuntimeUsesOnlyTheSelectedMerchantAccount(t *testing.T) {
	env := map[string]string{"GOEPAY_SMTP_HOST": "smtp.example.test", "GOEPAY_SMTP_PORT": "465", "GOEPAY_SMTP_SECURITY": "implicit", "GOEPAY_SMTP_USERNAME": "synthetic-user80", "GOEPAY_SMTP_PASSWORD": "synthetic-password80", "GOEPAY_SMTP_FROM": "no-reply@example.test", "GOEPAY_MERCHANT_TELEGRAM_TOKEN": "80001:synthetic80"}
	runtime := &merchantNoticeRuntime{getenv: func(key string) string { return env[key] }}
	for _, tc := range []struct {
		kind    notice.Transport
		account notice.Account
	}{
		{notice.SMTP, notice.Account{}},
		{notice.Robot, notice.Account{WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic80"}},
		{notice.Telegram, notice.Account{}},
		{notice.QQBot, notice.Account{QQBotAppID: "80001", QQBotAppSecret: "synthetic-merchant-secret80"}},
	} {
		sender, err := runtime.ResolveMerchantNotice(context.Background(), tc.kind, tc.account)
		if err != nil || sender == nil || sender.NoticeTransport() != tc.kind || sender.NoticeScope() == "" {
			t.Fatal("selected constructor unavailable", tc.kind, err)
		}
	}
	for _, kind := range []notice.Transport{notice.QQBot, notice.Robot, notice.Voice, notice.SMS, notice.Wechat} {
		if s, err := runtime.ResolveMerchantNotice(context.Background(), kind, notice.Account{}); err == nil || s != nil {
			t.Fatal("missing selected account fell back to platform SMTP", kind)
		}
	}
}

func TestMerchantNotice80RuntimeWechatRequiresTheSameOriginalAppAndTemplate(t *testing.T) {
	env := map[string]string{"GOEPAY_MERCHANT_WECHAT_APPID": "wxsynthetic80", "GOEPAY_MERCHANT_WECHAT_APPSECRET": "synthetic-secret80", "GOEPAY_CONSOLE_ORIGIN": "https://console.example.test"}
	runtime := &merchantNoticeRuntime{getenv: func(key string) string { return env[key] }}
	account := notice.Account{WXAppID: "wxsynthetic80", Template: notice.WechatTemplateConfig{ID: "synthetic-template80", Fields: map[string]string{"wxnotice_tpl_order_money": "amount1"}}, Input: notice.Input{Scene: "order", SiteName: "Local", At: time.Now(), Params: map[string]string{"name": "local", "money": "1.00", "type": "微信支付", "out_trade_no": "m80", "trade_no": "t80", "time": "2026-10-24 12:00:00"}}}
	if sender, err := runtime.ResolveMerchantNotice(context.Background(), notice.Wechat, account); err != nil || sender == nil || sender.NoticeTransport() != notice.Wechat {
		t.Fatal("configured original account unavailable", err)
	}
	account.WXAppID = "wxanother80"
	if sender, err := runtime.ResolveMerchantNotice(context.Background(), notice.Wechat, account); err == nil || sender != nil {
		t.Fatal("OpenID from another app adopted")
	}
}

func TestMerchantNotice80RuntimeVoiceRequiresAnExplicitTLSOutlet(t *testing.T) {
	env := map[string]string{"GOEPAY_MERCHANT_VOICE_HTTPS_ENDPOINT": "https://iot.solomo-info.com/admin/common/msgpush", "GOEPAY_MERCHANT_VOICE_AGENT_ID": "synthetic-agent80", "GOEPAY_MERCHANT_VOICE_SECRET": "synthetic-secret80"}
	runtime := &merchantNoticeRuntime{getenv: func(key string) string { return env[key] }}
	account := notice.Account{Input: notice.Input{Scene: "order", SiteName: "Local", At: time.Now(), Params: map[string]string{"name": "local", "money": "1.00", "type": "微信支付", "out_trade_no": "m80", "trade_no": "t80", "time": "2026-10-24 12:00:00"}}}
	if sender, err := runtime.ResolveMerchantNotice(context.Background(), notice.Voice, account); err != nil || sender == nil || sender.NoticeTransport() != notice.Voice {
		t.Fatal("explicit TLS voice outlet unavailable", err)
	}
	delete(env, "GOEPAY_MERCHANT_VOICE_HTTPS_ENDPOINT")
	if sender, err := runtime.ResolveMerchantNotice(context.Background(), notice.Voice, account); err == nil || sender != nil {
		t.Fatal("missing outlet used original plaintext endpoint")
	}
}
