package riskpolicy

import "testing"

func TestRisk177OriginalEmptyAndOffDefaults(t *testing.T) {
	v := Defaults()
	if len(v) != 25 || v["pay_iplimit"] != "" || v["pay_ip_interval"] != "" || v["pay_daymoney"] != "" || v["pay_block_oversea"] != "0" || v["auto_check_payip"] != "0" {
		t.Fatal("original default contract", v)
	}
}
func TestRisk177CanonicalExactIPAndMoneyNotSubstringOrFloat(t *testing.T) {
	v := Defaults()
	v["pay_iplimit_white"] = " 2001:0db8::1|::ffff:192.0.2.1|192.0.2.1 "
	v["pay_daymoney"] = "12.30"
	v["pay_region_block"] = "广东省|深圳市"
	n, e := Normalize(v)
	if e != nil || n["pay_iplimit_white"] != "192.0.2.1|2001:db8::1" || n["pay_daymoney"] != "12.30" {
		t.Fatal(n, e)
	}
	if !IPWhitelisted(n, "::ffff:192.0.2.1") || IPWhitelisted(n, "192.0.2.10") {
		t.Fatal("address identity or exact whitelist boundary lost")
	}
}
func TestRisk177RejectsMalformedOrFalselyEnabledRules(t *testing.T) {
	for _, tc := range []struct{ key, value string }{{"unknown", "1"}, {"pay_iplimit", "-1"}, {"pay_iplimit", "01"}, {"pay_ip_interval", "1.5"}, {"pay_daymoney", "1e2"}, {"pay_daymoney", "0.001"}, {"pay_block_oversea", "true"}, {"pay_iplimit_white", "192.0.2.1/24"}, {"pay_iplimit_white", "2001:db8::1%eth0"}, {"auto_check_payip", "1"}, {"check_sucrate_value", "100.01"}} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			v := Defaults()
			v[tc.key] = tc.value
			if _, e := Normalize(v); e == nil {
				t.Fatal("invalid original rule accepted")
			}
		})
	}
}
