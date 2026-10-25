package payment

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

func TestSettlementThreshold74TLSWholeWalletOriginalNetAndRevokedUnknownRecovery(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run := thresholdPreparedRun74(t, s, 7432, func() {
		if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{transfer_api_enabled}','true') WHERE id=7432`); e != nil {
			t.Fatal(e)
		}
		transferSetting(t, s, "transfer_enabled", "true")
		transferSetting(t, s, "settlement_fee_rate_bps", "300")
		transferSetting(t, s, "settlement_fee_maximum_cents", "25")
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7432, OutTradeNo: "threshold-tls-income", Amount: 1000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.MarkPaid(ctx, o.TradeNo, "threshold-tls-local"); e != nil {
			t.Fatal(e)
		}
	})
	if e := s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
		t.Fatal(e)
	}
	r, e := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if e != nil || len(r.Items) != 1 || r.Items[0].Gross != 1000 || r.Items[0].Fee != 25 || r.Items[0].Net != 975 {
		t.Fatal(r, e)
	}
	provider := &autoScopeProvider73{kind: "alipay", account: "seller:2088000000000073", product: "alipay.fund.trans.toaccount.transfer"}
	g := ProviderFlow{Store: s, AllowLive: true, Providers: map[int64]adapter.Provider{7: provider}, MerchantChannels: map[int64][]int64{7432: {7}}}
	if _, e = g.ExecuteSettlementAuto(ctx, r.Items[0].RequestID, nil); e == nil || provider.transfers.Load() != 0 {
		t.Fatal("threshold generation implied payment authority", e)
	}
	gen, e := s.readSettlementPlanAuthorization(ctx, s.pool, run.AuthorizationID)
	if e != nil {
		t.Fatal(e)
	}
	guarded := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, _ string) error {
		if kind != "settlement-auto-authorize" {
			return ErrInvalid
		}
		return nil
	})
	a, e := g.AuthorizeSettlementAuto(guarded, run.PlanID, run.PlanRevision, SettlementAutoInput{GenerationAuthorizationID: gen.View.ID, ValidFrom: gen.View.ValidFrom, ExpiresAt: gen.View.ExpiresAt, MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 1, CostPolicy: "provider_managed_excluded", Channels: []SettlementAutoChannel{{MerchantID: 7432, ChannelID: 7}}}, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	exerciseOriginalAutoTLS(t, s, g, a, r.Items[0].RequestID, 7432, 0, "9.75")
}
