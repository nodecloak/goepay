package main

import (
	"strings"
	"testing"
)

func smtpEnvironment() map[string]string {
	return map[string]string{"GOEPAY_ENABLE_EMAIL_VERIFICATION": "true", "GOEPAY_SMTP_HOST": "smtp.example.invalid", "GOEPAY_SMTP_PORT": "465", "GOEPAY_SMTP_SECURITY": "implicit", "GOEPAY_SMTP_USERNAME": "synthetic-user", "GOEPAY_SMTP_PASSWORD": "synthetic-password", "GOEPAY_SMTP_FROM": "sender@example.test"}
}
func TestVerificationSMTPExplicitGateAndZeroIO(t *testing.T) {
	for _, gate := range []string{"", "false"} {
		m := smtpEnvironment()
		m["GOEPAY_ENABLE_EMAIL_VERIFICATION"] = gate
		m["GOEPAY_SMTP_HOST"] = "bad://@host"
		sender, e := configuredVerificationSender(func(k string) string { return m[k] })
		if e != nil || sender != nil {
			t.Fatal("disabled SMTP must stay nil")
		}
	}
	m := smtpEnvironment()
	sender, e := configuredVerificationSender(func(k string) string { return m[k] })
	if e != nil || sender == nil {
		t.Fatal("valid explicit SMTP rejected", e)
	}
	for _, security := range []string{"starttls", "implicit"} {
		m["GOEPAY_SMTP_SECURITY"] = security
		if _, e = configuredVerificationSender(func(k string) string { return m[k] }); e != nil {
			t.Fatal(e)
		}
	}
}
func TestVerificationSMTPConfigurationFailsClosedAndRedacted(t *testing.T) {
	for _, change := range [][2]string{{"GOEPAY_ENABLE_EMAIL_VERIFICATION", "TRUE"}, {"GOEPAY_SMTP_HOST", "https://smtp.example.invalid"}, {"GOEPAY_SMTP_HOST", "x@host"}, {"GOEPAY_SMTP_HOST", "host\r\nDATA"}, {"GOEPAY_SMTP_PORT", "0465"}, {"GOEPAY_SMTP_PORT", "0"}, {"GOEPAY_SMTP_PORT", "65536"}, {"GOEPAY_SMTP_PORT", ""}, {"GOEPAY_SMTP_SECURITY", "plain"}, {"GOEPAY_SMTP_SECURITY", ""}, {"GOEPAY_SMTP_USERNAME", ""}, {"GOEPAY_SMTP_PASSWORD", ""}, {"GOEPAY_SMTP_PASSWORD", "secret\r\n"}, {"GOEPAY_SMTP_FROM", "Name <sender@example.test>"}, {"GOEPAY_SMTP_FROM", "a@example.test,b@example.test"}} {
		m := smtpEnvironment()
		m[change[0]] = change[1]
		sender, e := configuredVerificationSender(func(k string) string { return m[k] })
		if sender != nil || e == nil {
			t.Fatal("invalid config accepted", change[0])
		}
		if strings.Contains(e.Error(), m["GOEPAY_SMTP_PASSWORD"]) && m["GOEPAY_SMTP_PASSWORD"] != "" {
			t.Fatal("secret in error")
		}
	}
}
