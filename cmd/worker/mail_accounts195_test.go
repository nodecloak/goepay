package main

import (
	"context"
	"goepay/internal/businessnotice"
	"goepay/internal/mailaccount195"
	"goepay/internal/merchantnotice"
	"testing"
)

type closedMail195 struct{ calls int }

func (s *closedMail195) ResolveSMTP(context.Context) (*mailaccount195.BoundSMTP, bool, error) {
	s.calls++
	return nil, true, mailaccount195.ErrSource
}

func TestMailAccounts195AllWorkerSMTPPathsDenyClosedManagedSourceInsteadOfDeploymentFallback(t *testing.T) {
	env := map[string]string{"GOEPAY_ENABLE_ADMIN_CHANNEL_NOTICES": "true", "GOEPAY_SMTP_HOST": "smtp.example.invalid", "GOEPAY_SMTP_PORT": "465", "GOEPAY_SMTP_SECURITY": "implicit", "GOEPAY_SMTP_USERNAME": "synthetic195-legacy", "GOEPAY_SMTP_PASSWORD": "synthetic195-legacy-password", "GOEPAY_SMTP_FROM": "legacy195@example.test", "GOEPAY_ADMIN_MAIL_RECV": "recipient195@example.test"}
	getenv := func(k string) string { return env[k] }
	t.Run("merchant", func(t *testing.T) {
		source := &closedMail195{}
		r := &merchantNoticeRuntime{getenv: getenv, mailAccounts: source}
		sender, e := r.ResolveMerchantNotice(context.Background(), merchantnotice.SMTP, merchantnotice.Account{})
		if e == nil || sender != nil || source.calls != 1 {
			t.Fatal("merchant SMTP ignored managed shutdown", e, source.calls)
		}
	})
	t.Run("administrator business", func(t *testing.T) {
		source := &closedMail195{}
		r := &businessNoticeRuntime{getenv: getenv, mailAccounts: source}
		route, e := r.ResolveBusinessNotice(context.Background(), merchantnotice.SMTP, businessnotice.Account{})
		if e == nil || route.Sender != nil || source.calls != 1 {
			t.Fatal("business SMTP ignored managed shutdown", e, source.calls)
		}
	})
	t.Run("channel stopped", func(t *testing.T) {
		source := &closedMail195{}
		r := &channelNoticeRuntime{getenv: getenv, settings: &noticeSettings77{values: map[string]string{"msgconfig_risk": "0"}}, mailAccounts: source}
		sender, _, e := r.Resolve(context.Background())
		if e == nil || sender != nil || source.calls != 1 {
			t.Fatal("channel SMTP ignored managed shutdown", e, source.calls)
		}
	})
}
