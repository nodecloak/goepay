package server

import "testing"

func TestSettlementAuto73FundsTargetStrictBodyAndAlwaysMandatory(t *testing.T) {
	d, ok := fundsTarget("/console/v1/admin/settlement-plans/7/auto-authorizations")
	if !ok || d.guard != "settlement-auto-authorize" || !mandatoryFundsAuthorization(d) {
		t.Fatal("independent always-required auto funds action missing", d, ok)
	}
	good := `{"expected_revision":"2","expected_authorization_id":"0","generation_authorization_id":"3","valid_from":"2026-10-24T00:00:00Z","expires_at":"2026-10-24T01:00:00Z","max_item_gross":"100","max_run_gross":"200","max_day_gross":"300","max_day_items":"2","cost_policy":"provider_managed_excluded","channels":[{"merchant_id":"1","channel_id":"7"}]}`
	if _, _, e := normalizeFundsBody([]byte(good), d); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{`null`, `[]`, good[:len(good)-1] + `,"provider_fee":"0"}`, good[:len(good)-1] + `,"cost_policy":"free"}`, `{"expected_revision":"2"}`} {
		if _, _, e := normalizeFundsBody([]byte(bad), d); e == nil {
			t.Fatal("unsafe auto authority body", bad)
		}
	}
	for _, target := range []string{"/console/v1/admin/settlement-plans/07/auto-authorizations", "/console/v1/admin/settlement-plans/7/auto-authorizations?x=1", "/console/v1/admin/settlement-plans/7/auto-authorizations/1/revoke"} {
		if _, ok := fundsTarget(target); ok {
			t.Fatal("ambiguous or reduce-only action became a new payout authority", target)
		}
	}
}
