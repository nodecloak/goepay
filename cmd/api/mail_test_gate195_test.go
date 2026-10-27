package main

import "testing"

func TestSMTPTest195ExplicitGateDefaultOffAndNoCredentialRead(t *testing.T) {
	for _, raw := range []string{"", "false", "true", "TRUE", "1"} {
		reads := 0
		enabled, e := mailTestGate195(func(key string) string {
			if key != "GOEPAY_ENABLE_SMTP_TEST" {
				reads++
				return "must-not-read-private-account"
			}
			return raw
		})
		if reads != 0 || enabled != (raw == "true") || ((raw == "" || raw == "true" || raw == "false") != (e == nil)) {
			t.Fatal("SMTP test gate not exact/default-off", raw, enabled, reads, e)
		}
	}
}
