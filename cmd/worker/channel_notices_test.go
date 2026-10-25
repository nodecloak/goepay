package main

import (
	"strings"
	"testing"
)

func noticeEnvironment76() map[string]string {
	return map[string]string{"GOEPAY_ENABLE_ADMIN_CHANNEL_NOTICES": "true", "GOEPAY_ADMIN_NOTICE_RECIPIENT": "local-admin@example.test", "GOEPAY_SMTP_HOST": "smtp.example.invalid", "GOEPAY_SMTP_PORT": "465", "GOEPAY_SMTP_SECURITY": "implicit", "GOEPAY_SMTP_USERNAME": "synthetic-user", "GOEPAY_SMTP_PASSWORD": "synthetic-notice-password76", "GOEPAY_SMTP_FROM": "sender@example.test"}
}
func TestChannelNotice76ConfigurationDefaultsOffAndRequiresExplicitTLSAndRecipient(t *testing.T) {
	for _, flag := range []string{"", "false"} {
		m := noticeEnvironment76()
		m["GOEPAY_ENABLE_ADMIN_CHANNEL_NOTICES"] = flag
		m["GOEPAY_SMTP_HOST"] = "must-not-resolve://"
		sender, to, e := configuredChannelNotices(func(k string) string { return m[k] })
		if e != nil || sender != nil || to != "" {
			t.Fatal("disabled notices read invalid transport config", e)
		}
	}
	m := noticeEnvironment76()
	sender, to, e := configuredChannelNotices(func(k string) string { return m[k] })
	if e != nil || sender == nil || to != "local-admin@example.test" {
		t.Fatal("explicit notice config unavailable", e)
	}
	for _, change := range [][2]string{{"GOEPAY_ENABLE_ADMIN_CHANNEL_NOTICES", "TRUE"}, {"GOEPAY_ADMIN_NOTICE_RECIPIENT", ""}, {"GOEPAY_ADMIN_NOTICE_RECIPIENT", "a@example.test,b@example.test"}, {"GOEPAY_ADMIN_NOTICE_RECIPIENT", "a@example.test\r\nBcc:other@example.test"}, {"GOEPAY_SMTP_PORT", "0465"}, {"GOEPAY_SMTP_SECURITY", "plain"}, {"GOEPAY_SMTP_PASSWORD", ""}} {
		m := noticeEnvironment76()
		m[change[0]] = change[1]
		sender, to, e := configuredChannelNotices(func(k string) string { return m[k] })
		if sender != nil || to != "" || e == nil {
			t.Fatal("unsafe notice config accepted", change[0])
		}
		if strings.Contains(e.Error(), "synthetic-notice-password76") {
			t.Fatal("configuration error leaked credential")
		}
	}
}
