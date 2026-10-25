package server

import "testing"

func TestMembershipReferral72FundsPolicyIndependentTargetAndStrictCanonicalBody(t *testing.T) {
	d, ok := fundsTarget("/console/v1/admin/membership-referral-policies/global/0")
	if !ok || d.guard != "membership-referral-policy" || d.category != "membership-referral-policy" {
		t.Fatal("independent membership policy target missing", d, ok)
	}
	valid := `{"expected":"0","enabled":true,"rate_bps":2500}`
	if _, _, e := normalizeFundsBody([]byte(valid), d); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"expected":0,"enabled":true,"rate_bps":2500}`, `{"expected":"00","enabled":true,"rate_bps":2500}`, `{"expected":"0","enabled":null,"rate_bps":2500}`, `{"expected":"0","enabled":true,"rate_bps":10001}`, `{"expected":"0","enabled":true,"rate_bps":"2500"}`, `{"expected":"0","enabled":true,"rate_bps":2500,"basis":"profit"}`, `{"expected":"0","expected":"1","enabled":true,"rate_bps":2500}`} {
		if _, _, e := normalizeFundsBody([]byte(raw), d); e == nil {
			t.Fatal("invalid independent policy body accepted", raw)
		}
	}
	group, ok := fundsTarget("/console/v1/admin/membership-referral-policies/group/12")
	if !ok {
		t.Fatal("group policy missing")
	}
	if _, _, e := normalizeFundsBody([]byte(`{"expected":"0","enabled":null,"rate_bps":null}`), group); e != nil {
		t.Fatal("explicit group inheritance rejected", e)
	}
}
