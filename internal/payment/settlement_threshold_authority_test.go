package payment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSettlementThreshold74DefinitionAndBoundedAuthorityKeepOriginalModeSeparate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 7403)
	in := settlementPlanInputFor(7403)
	in.Mode = "balance_threshold"
	in.Schedule = SettlementScheduleSpec{Frequency: "minute", Interval: 1, AnchorAt: time.Now().UTC().Truncate(time.Minute)}
	p, e := s.UpdateSettlementPlan(ctx, p.ID, p.Revision, in, settlementPlanAdmin)
	if e != nil || p.Mode != "balance_threshold" || p.Enabled {
		t.Fatal("new threshold definition must be distinct and disabled", p, e)
	}
	guard := settlementPlanGuard(t, s)
	budget := settlementPlanAuthorizationInput(t, s)
	if _, e = s.AuthorizeSettlementPlan(guard, p.ID, p.Revision, budget, settlementPlanAdmin); !errors.Is(e, ErrState) {
		t.Fatal("unset threshold granted future withdrawals", e)
	}
	for key, value := range map[string]string{"auto_settle_money": "100.25", "transfer_alipay": "7", "settlement_fee_rate_bps": "300", "settlement_fee_maximum_cents": "250"} {
		transferSetting(t, s, key, value)
	}
	a, e := s.AuthorizeSettlementPlan(guard, p.ID, p.Revision, budget, settlementPlanAdmin)
	if e != nil {
		t.Fatal("finite threshold generation authority", e)
	}
	sealed, e := s.readSettlementPlanAuthorization(ctx, s.pool, a.ID)
	if e != nil || sealed.Scope.Threshold == nil || sealed.Scope.Threshold.ChannelID != 7 || sealed.Scope.Threshold.Policy.ThresholdCents != 10025 || sealed.Scope.Merchants[0].Threshold == nil {
		t.Fatal("threshold authority omitted original configuration/group scope", sealed, e)
	}
	if sealed.Scope.Policy.FeeMinimumCents != 0 || sealed.Scope.Policy.RetainedCents != 0 {
		t.Fatal("threshold adopted normal Settle floor/retention")
	}
	switched := settlementPlanInputFor(7403)
	if _, e = s.UpdateSettlementPlan(ctx, p.ID, p.Revision, switched, settlementPlanAdmin); !errors.Is(e, ErrState) {
		t.Fatal("an authorized plan crossed business families and could reset threshold budget history", e)
	}
	transferSetting(t, s, "auto_settle_money", "0")
	if _, e = s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin); e == nil {
		t.Fatal("disabled or replaced original threshold scope generated a new run")
	}
	var grants, journals, jobs int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM local_plan_grants),(SELECT count(*) FROM journals),(SELECT count(*) FROM jobs)").Scan(&grants, &journals, &jobs); e != nil || grants != 1 || journals != 0 || jobs != 0 {
		t.Fatal("definition/scope caused payment or consumed invalid grants", grants, journals, jobs, e)
	}
}
