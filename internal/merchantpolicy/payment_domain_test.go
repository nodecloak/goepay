package merchantpolicy

import "testing"

func TestPaymentDomainOverrideAndReviewedRuleMatching(t *testing.T) {
	for _, value := range []string{"", "_", "0", "1"} {
		v, e := ParsePaymentDomainOverride(value)
		if e != nil || v.Override != (value == "0" || value == "1") || v.Enabled != (value == "1") {
			t.Fatal(value, v, e)
		}
	}
	for _, value := range []string{"true", "false", " 1", "01", "2"} {
		if _, e := ParsePaymentDomainOverride(value); e == nil {
			t.Error("invalid domain switch accepted", value)
		}
	}
	for _, tc := range []struct {
		host, rule string
		match      bool
	}{
		{"SHOP.Example.COM.", "example.com", true}, {"nested.shop.example.com", "*.example.com", true}, {"example.com", "*.example.com", false}, {"evil-example.com", "example.com", false}, {"example.com.evil.org", "example.com", false}, {"127.0.0.1", "127.0.0.1", false}, {"shop.example.com", "https://shop.example.com", false}, {"*.example.com", "*.example.com", false},
	} {
		if actual := AuthorizationDomainMatches(tc.host, tc.rule); actual != tc.match {
			t.Error(tc.host, tc.rule, actual)
		}
	}
}
