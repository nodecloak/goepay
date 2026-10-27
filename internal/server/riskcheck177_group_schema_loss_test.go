package server

import (
	"goepay/internal/riskgeo"
	"net/netip"
	"testing"
)

func TestRisk177GroupOnlyConfiguredSchemaLossFailsClosed(t *testing.T) {
	f, _, creates := risk177Flow(t)
	f.s.Store.CoreRiskGeo = risk177GeoFunc(func(netip.Addr) (riskgeo.Region, bool, error) {
		return riskgeo.Region{Country: "美国", Province: "synthetic", City: "synthetic"}, true, nil
	})
	group := risk177Data(t, f.riskPath("GET", "/groups/0", ""))
	risk177Data(t, f.riskPath("PATCH", "/groups/0", risk177GroupBody(t, group, "1")))
	var global, groupAudit int
	if e := f.s.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM backoffice_audit WHERE resource='core-payment-risk'),(SELECT count(*) FROM backoffice_audit WHERE resource='core-payment-risk-groups')`).Scan(&global, &groupAudit); e != nil || global != 0 || groupAudit != 1 {
		t.Fatal("not a genuine group-only configuration", global, groupAudit, e)
	}
	// Simulate a restored/incomplete schema while the independent group policy
	// audit survives; no old policy/origin audit is invented to mask this case.
	if _, e := f.s.Pool.Exec(t.Context(), `DROP TABLE core_payment_risk_policy,core_risk_origins,core_risk_successes,core_risk_actions,core_risk_check_runs,core_risk_notices,core_risk_notice_claims,core_risk_notice_results,core_payment_risk_groups CASCADE;ALTER TABLE provider_attempts DROP COLUMN core_risk_required CASCADE;DELETE FROM schema_migrations WHERE name='000102_core_payment_risk.up.sql'`); e != nil {
		t.Fatal(e)
	}
	out := f.pay(t, "root-group-schema-loss177", "192.0.2.177")
	t.Log("actual payment after original group-only risk sources lost", out["code"], "provider creates", creates.Load())
	if out["code"] == float64(1) || creates.Load() != 0 {
		t.Fatal("independently anchored group risk was silently downgraded to unconfigured legacy")
	}
}
