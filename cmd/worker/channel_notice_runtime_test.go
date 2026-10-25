package main

import (
	"context"
	"errors"
	"goepay/internal/payment"
	"testing"
)

type noticeSettings77 struct {
	values map[string]string
	calls  int
	err    error
}

func (r *noticeSettings77) AdminChannelNoticeSettings(context.Context) (map[string]string, error) {
	r.calls++
	return r.values, r.err
}

func TestChannelNotice77WorkerReadsCurrentOriginalSettingsButKeepsTokensInsideSendingProfile(t *testing.T) {
	m := noticeEnvironment76()
	m["GOEPAY_SMTP_HOST"] = "unused-invalid://"
	m["GOEPAY_ADMIN_LOGIN_TELEGRAM_TOKEN"] = "12345:synthetic77"
	settings := &noticeSettings77{values: map[string]string{"msgconfig_risk": "2", "msgtelegram_chatid": "-12345"}}
	r, e := newChannelNoticeRuntime(context.Background(), settings, func(k string) string { return m[k] })
	if e != nil || r == nil {
		t.Fatal(e)
	}
	s, to, e := r.Resolve(context.Background())
	if e != nil || settings.calls < 1 || to != "-12345" || s.(payment.ChannelNoticeRoute).ChannelNoticeMode() != "telegram" {
		t.Fatal("worker ignored persisted Pro selector", to, e)
	}
	settings.values["msgtelegram_chatid"] = "-54321"
	next, to, e := r.Resolve(context.Background())
	if e != nil || next != s || to != "-54321" {
		t.Fatal("new recipient not read or same sending profile lost token cache", e)
	}
	m["GOEPAY_ADMIN_LOGIN_TELEGRAM_TOKEN"] = "12345:changed77"
	next, _, e = r.Resolve(context.Background())
	if e != nil || next == s {
		t.Fatal("changed credentials reused previous profile", e)
	}
	settings.values = map[string]string{"msgconfig_risk": "0", "mail_recv": "current-admin@example.test"}
	m["GOEPAY_SMTP_HOST"] = "smtp.example.invalid"
	next, to, e = r.Resolve(context.Background())
	if e != nil || to != "current-admin@example.test" || next.(payment.ChannelNoticeRoute).ChannelNoticeMode() != "smtp" {
		t.Fatal("current mail setting ignored", e)
	}
	settings.err = errors.New("synthetic malformed original selector")
	if sender, to, e := r.Resolve(context.Background()); e != errChannelNoticeConfiguration || sender != nil || to != "" {
		t.Fatal("failed settings read reused stale transport", e)
	}
}

func TestChannelNotice77DisabledWorkerNeverReadsNotificationSettingsOrSecrets(t *testing.T) {
	for _, flag := range []string{"", "false"} {
		settings := &noticeSettings77{err: errors.New("must not read")}
		r, e := newChannelNoticeRuntime(context.Background(), settings, func(k string) string {
			if k == "GOEPAY_ENABLE_ADMIN_CHANNEL_NOTICES" {
				return flag
			}
			t.Error("disabled worker read profile", k)
			return ""
		})
		if e != nil || r != nil || settings.calls != 0 {
			t.Fatal("disabled configuration performed work", e)
		}
	}
}

func TestChannelNotice77EquivalentWebhookProfileStillUsesTheCurrentExactTarget(t *testing.T) {
	m := noticeEnvironment76()
	m["GOEPAY_ADMIN_MSGROBOT_URL"] = "https://oapi.dingtalk.com/robot/send?access_token=synthetic77"
	settings := &noticeSettings77{values: map[string]string{"msgconfig_risk": "1"}}
	r, e := newChannelNoticeRuntime(context.Background(), settings, func(k string) string { return m[k] })
	if e != nil {
		t.Fatal(e)
	}
	before, _, e := r.Resolve(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	m["GOEPAY_ADMIN_MSGROBOT_URL"] = "https://OAPI.DINGTALK.COM:443/robot/send?access_token=synthetic77"
	after, to, e := r.Resolve(context.Background())
	if e != nil || after.(payment.ChannelNoticeRoute).ValidateChannelNoticeRecipient(to) != nil {
		t.Fatal("cached equivalent profile rejected the current exact target", e)
	}
	if before.(payment.ChannelNoticeRoute).ChannelNoticeScope() != after.(payment.ChannelNoticeRoute).ChannelNoticeScope() {
		t.Fatal("equivalent endpoint changed identity")
	}
}
