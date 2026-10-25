package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"sync"
	"testing"
)

func TestSettlementAuto73OriginalRunReserveSingleUnknownSendAndNoGenerationOnlyAuthority(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 7392)
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{transfer_api_enabled}','true') WHERE id=7392`); e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "transfer_enabled", "true")
	transferSetting(t, s, "settlement_fee_rate_bps", "300")
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7392, OutTradeNo: "auto73-income", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "auto73-local-credit"); e != nil {
		t.Fatal(e)
	}
	gen, e := s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, p.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin)
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
	rv, e := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if e != nil || len(rv.Items) != 1 || rv.Items[0].State != "created" {
		t.Fatal(rv, e)
	}
	req := rv.Items[0].RequestID
	provider := &autoScopeProvider73{kind: "alipay", account: "seller:2088000000000073", product: "alipay.fund.trans.toaccount.transfer"}
	provider.unknown = true
	g := ProviderFlow{Store: s, AllowLive: true, MerchantChannels: map[int64][]int64{7392: {7}}, Providers: map[int64]adapter.Provider{7: provider}}
	if _, e = g.ExecuteSettlementAuto(ctx, req, nil); e == nil || provider.transfers.Load() != 0 {
		t.Fatal("generation-only authority sent", e)
	}
	in := SettlementAutoInput{GenerationAuthorizationID: gen.ID, ValidFrom: gen.ValidFrom, ExpiresAt: gen.ExpiresAt, MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 1, CostPolicy: "provider_managed_excluded", Channels: []SettlementAutoChannel{{MerchantID: 7392, ChannelID: 7}}}
	guard := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, _ string) error {
		if kind != "settlement-auto-authorize" {
			return ErrInvalid
		}
		return nil
	})
	a, e := g.AuthorizeSettlementAuto(guard, p.ID, p.Revision, in, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, e := g.ExecuteSettlementAuto(ctx, req, nil)
			var unknown *adapter.UnknownOutcome
			if e != nil && (!errors.As(e, &unknown) || out.State != "unknown") {
				t.Error("unexpected automatic claim error", e)
			}
		}()
	}
	wg.Wait()
	if provider.transfers.Load() != 1 {
		t.Fatal("duplicate automatic send", provider.transfers.Load())
	}
	var claims, gross, net, fee int64
	if e = s.pool.QueryRow(ctx, "SELECT count(*),COALESCE(sum(gross),0),COALESCE(sum(net),0),COALESCE(sum(fee),0) FROM settlement_auto_claims").Scan(&claims, &gross, &net, &fee); e != nil || claims != 1 || gross != 1000 || net != 970 || fee != 30 {
		t.Fatal("bounded original budget", claims, gross, net, fee, e)
	}
	b, e := s.Balance(ctx, 7392)
	if e != nil || b.Available != 1000 || b.Reserved != 1000 {
		t.Fatal("auto payout debited wallet again or released unknown", b, e)
	}
	if e = s.RevokeSettlementAuto(ctx, a.ID, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	if _, e = g.ExecuteSettlementAuto(ctx, req, nil); e != nil || provider.transfers.Load() != 1 {
		t.Fatal("revocation rearmed original unknown", e)
	}
	check, e := s.CheckLedger(ctx)
	if e != nil || check.InvalidSettlementPlans != 0 || check.InvalidSettlementFundings != 0 || check.InvalidJournals != 0 || check.InvalidAccounts != 0 {
		t.Fatal("automatic source checker", check, e)
	}
	g.AllowLive = false
	resolved, e := g.QuerySettlementPayout(ctx, req, settlementAdmin)
	if e != nil || resolved.State != "succeeded" || provider.transfers.Load() != 1 || provider.query.Load() != 1 {
		t.Fatal("revoked automatic unknown could not query original TF", resolved, e)
	}
	b, e = s.Balance(ctx, 7392)
	if e != nil || b.Available != 1000 || b.Reserved != 0 {
		t.Fatal("recovered original net+fee accounting", b, e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE settlement_auto_claims SET proof_hash=decode(repeat('00',32),'hex') WHERE request_id=$1", req); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = g.QuerySettlementPayout(ctx, req, settlementAdmin); !errors.Is(e, ErrConflict) || provider.query.Load() != 1 {
		t.Fatal("damaged automatic source reached provider query", e)
	}
	check, e = s.CheckLedger(ctx)
	if e != nil || check.InvalidSettlementPlans == 0 {
		t.Fatal("automatic claim proof omitted from checker", check, e)
	}
}
