package main

import (
	"context"
	"errors"
	"goepay/internal/businessnotice"
	"goepay/internal/merchantnotice"
	"goepay/internal/voiceconfig"
	"testing"
	"time"
)

func TestNotice230BusinessMailUsesSavedNonemptyFallback(t *testing.T) {
	env := map[string]string{"GOEPAY_SMTP_HOST": "smtp.example.test", "GOEPAY_SMTP_PORT": "465", "GOEPAY_SMTP_SECURITY": "implicit", "GOEPAY_SMTP_USERNAME": "local230", "GOEPAY_SMTP_PASSWORD": "synthetic230", "GOEPAY_SMTP_FROM": "local@example.test"}
	r := &businessNoticeRuntime{getenv: func(k string) string { return env[k] }}
	out, e := r.ResolveBusinessNotice(context.Background(), merchantnotice.SMTP, businessnotice.Account{Settings: map[string]string{"mail_recv": "", "mail_name": "fallback230@example.test"}})
	if e != nil || out.Recipient != "fallback230@example.test" {
		t.Fatal("saved mail fallback ignored", e)
	}
}
func TestNotice230RiskMailHonorsManagedShutdown(t *testing.T) {
	source := &closedMail195{}
	r := &coreRiskNoticeRuntime{getenv: func(k string) string {
		if k == "GOEPAY_ENABLE_CORE_RISK_NOTICES" {
			return "true"
		}
		return ""
	}, mailAccounts: source}
	sender, _, e := r.ResolveCoreRiskNotice(context.Background(), "merchant")
	if e == nil || sender != nil || source.calls != 1 {
		t.Fatal("risk notice bypassed managed mail", e, source.calls)
	}
}

type voice230Source struct {
	managed, enabled bool
	err              error
	calls            int
}

func (s *voice230Source) Resolve(context.Context) (voiceconfig.View, voiceconfig.Credential, bool, error) {
	s.calls++
	return voiceconfig.View{Enabled: s.enabled}, voiceconfig.Credential{AgentID: "managed230", Secret: "managed-secret230"}, s.managed, s.err
}
func TestNotice230VoiceManagedDisabledOrCorruptNeverUsesEnvironmentFallback(t *testing.T) {
	env := map[string]string{"GOEPAY_MERCHANT_VOICE_HTTPS_ENDPOINT": "https://iot.solomo-info.com/admin/common/msgpush", "GOEPAY_MERCHANT_VOICE_AGENT_ID": "env230", "GOEPAY_MERCHANT_VOICE_SECRET": "env-secret230"}
	a := merchantnotice.Account{Input: merchantnotice.Input{Scene: "order", SiteName: "Local", At: time.Now(), Params: map[string]string{"name": "local", "money": "1.00", "type": "微信支付", "out_trade_no": "m230", "trade_no": "t230", "time": "2026-11-02 12:00:00"}}}
	for _, source := range []*voice230Source{{managed: true}, {managed: true, enabled: true, err: errors.New("protected source damaged")}, {managed: true, enabled: true}} {
		r := &merchantNoticeRuntime{getenv: func(k string) string { return env[k] }, voiceConfig: source}
		sender, e := r.ResolveMerchantNotice(context.Background(), merchantnotice.Voice, a)
		if source.enabled && source.err == nil {
			if e != nil || sender == nil {
				t.Fatal("managed voice unavailable", e)
			}
		} else if e == nil || sender != nil {
			t.Fatal("managed disabled/corrupt voice fell back")
		}
		if source.calls != 1 {
			t.Fatal("managed voice source not read once")
		}
	}
}
