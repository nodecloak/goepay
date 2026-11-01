package merchantpolicy

import (
	"encoding/json"
	"testing"
)

func TestDepositPolicyTypedOverridesAndLegacyGlobalDefaults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		global  map[string]string
		body    string
		enabled bool
		minimum int64
		days    int
	}{
		{"default", nil, `{}`, true, 0, 0},
		{"legacy_bad_numeric", map[string]string{"user_deposit": "old-nonzero", "user_deposit_min": "bad", "user_deposit_day": "bad"}, `{}`, true, 0, 0},
		{"inherit", map[string]string{"user_deposit": "0", "user_deposit_min": "12.34", "user_deposit_day": "7"}, `{"user_deposit":"_","user_deposit_min":"","user_deposit_day":"_"}`, false, 1234, 7},
		{"explicit_zero", map[string]string{"user_deposit_min": "99.99", "user_deposit_day": "30"}, `{"user_deposit":"0","user_deposit_min":"0","user_deposit_day":"0"}`, false, 0, 0},
		{"member_enable", map[string]string{"user_deposit": "0"}, `{"user_deposit":"1","user_deposit_min":"0.01","user_deposit_day":"3660"}`, true, 1, 3660},
		{"legacy_json_number", nil, `{"user_deposit":0,"user_deposit_min":0.1,"user_deposit_day":7}`, false, 10, 7},
		{"exact_maximum", nil, `{"user_deposit_min":"92233720368547758.07"}`, true, 9223372036854775807, 0},
		{"legacy_global_format", map[string]string{"user_deposit_min": "001.0", "user_deposit_day": "+7"}, `{}`, true, 100, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, e := ResolveDepositPolicy(tc.global, json.RawMessage(tc.body))
			if e != nil || p.Enabled != tc.enabled || p.MinCents != tc.minimum || p.Days != tc.days {
				t.Fatalf("policy=%+v err=%v", p, e)
			}
		})
	}
	for _, body := range []string{`null`, `[]`, `{"user_deposit":null}`, `{"user_deposit":true}`, `{"user_deposit":"2"}`, `{"user_deposit":" 0"}`, `{"user_deposit_min":null}`, `{"user_deposit_min":"abc"}`, `{"user_deposit_min":"-1"}`, `{"user_deposit_min":"1."}`, `{"user_deposit_min":"0.001"}`, `{"user_deposit_min":"92233720368547758.08"}`, `{"user_deposit_day":"01"}`, `{"user_deposit_day":"3661"}`, `{"user_deposit_day":1.5}`, `{"user_deposit_day":false}`} {
		if _, e := ResolveDepositPolicy(nil, json.RawMessage(body)); e == nil {
			t.Error("bad group deposit policy did not fail closed", body)
		}
	}
}
