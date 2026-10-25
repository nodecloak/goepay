package server

import (
	"strings"
	"testing"
)

func TestReferralProfit71PolicyNormalizerKeepsFrozenBodyCostServerOnly(t *testing.T) {
	d, ok := referralPolicyFundsTarget([]string{"referral-policies", "global", "0"})
	if !ok {
		t.Fatal("target")
	}
	body := `{"expected":"0","enabled":true,"rate_bps":5000,"mode":"immediate","basis":"profit","refund_policy":"any_successful_refund_voids_reward"}`
	fields, canonical, e := normalizeReferralPolicyFundsBody([]byte(body), d)
	if e != nil || len(fields) != 6 || !strings.Contains(string(canonical), `"basis":"profit"`) {
		t.Fatal(fields, string(canonical), e)
	}
	for _, extra := range []string{`"costrate":"0"`, `"payment_cost":{}`, `"profit_basis_available":true`} {
		if _, _, e = normalizeReferralPolicyFundsBody([]byte(strings.TrimSuffix(body, "}")+","+extra+"}"), d); e == nil {
			t.Fatal("client cost/capability injection accepted", extra)
		}
	}
}
