package backoffice

import (
	"testing"
	"time"
)

func TestActiveBlacklistReasonRetainsPaymentAdmissionSemantics(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, kind, value, expiry string
		p                         PaymentContext
		want                      string
	}{
		{"account permanent", "buyer", "account", "", PaymentContext{Buyer: "account"}, "buyer blocked"},
		{"expired at exact boundary", "buyer", "account", now.Format(time.RFC3339), PaymentContext{Buyer: "account"}, ""},
		{"malformed date stays active", "buyer", "account", "2000-01-01", PaymentContext{Buyer: "account"}, "buyer blocked"},
		{"invalid time stays active", "buyer", "account", "2000-01-01T24:00:00Z", PaymentContext{Buyer: "account"}, "buyer blocked"},
		{"unrelated kind", "buyer", "192.0.2.10", "", PaymentContext{ClientIP: "192.0.2.10"}, ""},
		{"network", "ip", "192.0.2.0/24", "", PaymentContext{ClientIP: "192.0.2.10"}, "client network blocked"},
		{"ipv6 equivalence", "ip", "2001:db8::1", "", PaymentContext{ClientIP: "2001:0db8:0:0:0:0:0:1"}, "client IP blocked"},
		{"noncanonical ipv4 is not admitted", "ip", "192.0.2.10", "", PaymentContext{ClientIP: "192.0.2.010"}, ""},
		{"callback host", "domain", "example.test", "", PaymentContext{NotifyURL: "https://example.test/notify"}, "callback domain blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := map[string]any{"kind": tc.kind, "value": tc.value, "expires_at": tc.expiry}
			if got := ActiveBlacklistReason(rule, tc.p, now); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
