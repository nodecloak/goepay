package server

import (
	"goepay/internal/mailaccount195"
	"testing"
)

func TestMailAccounts195NoticeResolverBindsExactRevisionAndCannotRestoreDisabledFallback(t *testing.T) {
	f := channel168Local(t)
	store := mailaccount195.New(f.p, f.s.CheckoutKey)
	if sender, managed, e := store.ResolveSMTP(t.Context()); e != nil || managed || sender != nil {
		t.Fatal("unconfigured source does not permit explicit legacy fallback", managed, e)
	}
	state := mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
	state = mail195Save(t, f, state, "smtp.example.invalid", "sender195@example.invalid", "synthetic195-notice-scope", 1, false)
	first, managed, e := store.ResolveSMTP(t.Context())
	if e != nil || !managed || first == nil || len(first.NoticeScope()) != 64 || first.NoticeScope() != first.ChannelNoticeScope() {
		t.Fatal("notice source revision not bound", managed, e)
	}
	state = mail195Save(t, f, state, "smtp.example.invalid", "sender195@example.invalid", "", 1, true)
	second, managed, e := store.ResolveSMTP(t.Context())
	if e != nil || !managed || second == nil || first.NoticeScope() == second.NoticeScope() {
		t.Fatal("same credentials in new revision reused original notice scope", e)
	}
	mail195Save(t, f, state, "smtp.example.invalid", "sender195@example.invalid", "", 0, true)
	if sender, managed, e := store.ResolveSMTP(t.Context()); e == nil || !managed || sender != nil {
		t.Fatal("closed managed notice account returned fallback", managed, e)
	}
}
