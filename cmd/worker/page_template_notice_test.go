package main

import (
	"goepay/internal/adapters/adminnotice"
	"goepay/internal/mailaccount195"
	"goepay/internal/mailer"
	"goepay/internal/templatenotice"
	"testing"
)

// The worker's admin.page-template-changed branch type-asserts the resolved
// payment.ChannelNoticeSender to templatenotice.ChangeNoticeSender. Every
// concrete transport the notice runtime can resolve must satisfy it, or the
// dispatch would silently fall through to a configuration error.
var (
	_ templatenotice.ChangeNoticeSender = (*adminnotice.Webhook)(nil)
	_ templatenotice.ChangeNoticeSender = (*adminnotice.Telegram)(nil)
	_ templatenotice.ChangeNoticeSender = (*adminnotice.QQBot)(nil)
	_ templatenotice.ChangeNoticeSender = (*mailer.SMTP)(nil)
	_ templatenotice.ChangeNoticeSender = (*mailaccount195.BoundSMTP)(nil)
)

func TestResolvedNoticeSendersImplementChangeNoticeSender(t *testing.T) {
	// The managed SMTP sender is a *mailaccount195.BoundSMTP, which embeds
	// *mailer.SMTP and so inherits SendPageTemplateChanged by promotion. The
	// var block above fails to compile if any transport loses the method.
	m := noticeEnvironment76()
	sender, _, e := configuredChannelNotices(func(k string) string { return m[k] })
	if e != nil || sender == nil {
		t.Fatal("explicit notice config unavailable", e)
	}
	if _, ok := sender.(templatenotice.ChangeNoticeSender); !ok {
		t.Fatal("resolved SMTP sender does not implement ChangeNoticeSender")
	}
}
