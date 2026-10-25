package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"testing"
)

func TestSettlementAuto73AuthorityReplacementCannotResetOriginalRunOrDaySpend(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 7393)
	settlementPlanReady(t, s, 7394)
	inPlan := settlementPlanInputFor(7393)
	inPlan.MerchantIDs = []int64{7393, 7394}
	p, e := s.UpdateSettlementPlan(ctx, p.ID, p.Revision, inPlan, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{transfer_api_enabled}','true') WHERE id IN(7393,7394)`); e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "transfer_enabled", "true")
	for _, mid := range []int64{7393, 7394} {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "auto73-budget-source", Amount: 2000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.MarkPaid(ctx, o.TradeNo, fmt.Sprint("auto73-budget-local-", mid)); e != nil {
			t.Fatal(e)
		}
	}
	genInput := settlementPlanAuthorizationInput(t, s)
	genInput.MaxRunGross = 2000
	genInput.MaxDayGross = 2000
	genInput.MaxDayItems = 2
	gen, e := s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, p.Revision, genInput, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
		t.Fatal(e)
	}
	v, e := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if e != nil || len(v.Items) != 2 {
		t.Fatal(v, e)
	}
	provider := &autoScopeProvider73{kind: "alipay", account: "seller:2088000000000073", product: "alipay.fund.trans.toaccount.transfer"}
	provider.unknown = true
	g := ProviderFlow{Store: s, AllowLive: true, MerchantChannels: map[int64][]int64{7393: {7}, 7394: {7}}, Providers: map[int64]adapter.Provider{7: provider}}
	guard := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, _ string) error {
		if kind != "settlement-auto-authorize" {
			return ErrInvalid
		}
		return nil
	})
	in := SettlementAutoInput{GenerationAuthorizationID: gen.ID, ValidFrom: gen.ValidFrom, ExpiresAt: gen.ExpiresAt, MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 2, CostPolicy: "provider_managed_excluded", Channels: []SettlementAutoChannel{{MerchantID: 7393, ChannelID: 7}, {MerchantID: 7394, ChannelID: 7}}}
	a, e := g.AuthorizeSettlementAuto(guard, p.ID, p.Revision, in, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	out, e := g.ExecuteSettlementAuto(ctx, v.Items[0].RequestID, nil)
	var unknown *adapter.UnknownOutcome
	if e != nil && (!errors.As(e, &unknown) || out.State != "unknown") {
		t.Fatal(e)
	}
	in.ExpectedAuthorizationID = a.ID
	a, e = g.AuthorizeSettlementAuto(guard, p.ID, p.Revision, in, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.ExecuteSettlementAuto(ctx, v.Items[1].RequestID, nil); !errors.Is(e, ErrState) || provider.transfers.Load() != 1 {
		t.Fatal("new authority reset original plan spend", provider.transfers.Load(), e)
	}
	var pending string
	if e = s.pool.QueryRow(ctx, "SELECT status FROM ledger_requests WHERE id=$1", v.Items[1].RequestID).Scan(&pending); e != nil || pending != "pending" {
		t.Fatal("bounded failure partly approved", pending, e)
	}
	in.ExpectedAuthorizationID = a.ID
	in.MaxRunGross = 2000
	in.MaxDayGross = 2000
	if _, e = g.AuthorizeSettlementAuto(guard, p.ID, p.Revision, in, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	out, e = g.ExecuteSettlementAuto(ctx, v.Items[1].RequestID, nil)
	if e != nil && (!errors.As(e, &unknown) || out.State != "unknown") {
		t.Fatal(e)
	}
	if provider.transfers.Load() != 2 {
		t.Fatal("new bounded authority did not use remaining original request", provider.transfers.Load())
	}
	check, e := s.CheckLedger(ctx)
	if e != nil || check.InvalidSettlementPlans != 0 || check.InvalidSettlementFundings != 0 {
		t.Fatal("later authority retroactively invalidated earlier legitimate budget", check, e)
	}
}
