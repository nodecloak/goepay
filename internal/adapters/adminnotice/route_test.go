package adminnotice

import (
	"encoding/hex"
	"testing"
)

type noticeRoute77 interface {
	ChannelNoticeMode() string
	ChannelNoticeScope() string
	ValidateChannelNoticeRecipient(string) error
}

func TestAdminNotice77RouteIdentityIsPureStableAndBindsCredentialAndTarget(t *testing.T) {
	hook, _ := NewWebhook("https://oapi.dingtalk.com/robot/send?access_token=synthetic77", nil)
	hook2, _ := NewWebhook("https://OAPI.DINGTALK.COM:443/robot/send?access_token=synthetic77", nil)
	hook3, _ := NewWebhook("https://oapi.dingtalk.com/robot/send?access_token=changed77", nil)
	tg, _ := NewTelegram("12345:synthetic77", nil)
	tg2, _ := NewTelegram("12345:changed77", nil)
	qq, _ := NewQQBot("app77", "synthetic-secret77", nil)
	qq2, _ := NewQQBot("app77", "changed-secret77", nil)
	for _, tc := range []struct {
		sender, same, changed any
		mode, to, bad         string
	}{
		{hook, hook2, hook3, "robot", "https://oapi.dingtalk.com/robot/send?access_token=synthetic77", "https://evil.invalid"},
		{tg, tg, tg2, "telegram", "-12345", "012345"},
		{qq, qq, qq2, "qqbot", "local_openid77", "openid/../other"},
	} {
		route, ok := tc.sender.(noticeRoute77)
		if !ok {
			t.Fatalf("%s sender lacks route identity", tc.mode)
		}
		same, ok := tc.same.(noticeRoute77)
		if !ok {
			t.Fatal("same route missing")
		}
		changed, ok := tc.changed.(noticeRoute77)
		if !ok {
			t.Fatal("changed route missing")
		}
		digest, e := hex.DecodeString(route.ChannelNoticeScope())
		if e != nil || len(digest) != 32 || route.ChannelNoticeMode() != tc.mode || route.ChannelNoticeScope() != same.ChannelNoticeScope() || route.ChannelNoticeScope() == changed.ChannelNoticeScope() {
			t.Fatal("route identity did not bind canonical sender", tc.mode)
		}
		if route.ValidateChannelNoticeRecipient(tc.to) != nil || route.ValidateChannelNoticeRecipient(tc.bad) == nil {
			t.Fatal("unsafe target accepted", tc.mode)
		}
	}
}
