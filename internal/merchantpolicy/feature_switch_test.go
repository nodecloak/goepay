package merchantpolicy

import "testing"

func TestGroupFeatureSwitchStrictInheritance(t *testing.T) {
	for _, tc := range []struct {
		body                string
		configured, enabled bool
	}{
		{`{}`, false, false}, {`{"recharge":""}`, false, false}, {`{"recharge":"_"}`, false, false},
		{`{"recharge":"0"}`, true, false}, {`{"recharge":"1"}`, true, true},
		{`{"recharge":0}`, true, false}, {`{"recharge":1}`, true, true},
	} {
		value, err := GroupFeatureSwitch([]byte(tc.body), "recharge")
		if err != nil || value.Configured != tc.configured || value.Enabled != tc.enabled {
			t.Fatalf("feature %s: %+v %v", tc.body, value, err)
		}
	}
	for _, body := range []string{`null`, `[]`, `{"recharge":null}`, `{"recharge":true}`, `{"recharge":"false"}`, `{"recharge":"2"}`, `{"recharge":" 1"}`, `{"recharge":{}}`} {
		if _, err := GroupFeatureSwitch([]byte(body), "recharge"); err == nil {
			t.Fatal("malformed group feature accepted", body)
		}
	}
	for _, key := range []string{"user_totp_open", "settle_open", "unknown", "pay_domain_open"} {
		if _, err := GroupFeatureSwitch([]byte(`{}`), key); err == nil {
			t.Fatal("out-of-scope key accepted", key)
		}
	}
}
