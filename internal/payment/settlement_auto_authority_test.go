package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"testing"
	"time"
)

func TestSettlementAuto73SeparateAuthorityOriginalGenerationAndCAS(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 7391)
	gen, e := s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, p.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	g := ProviderFlow{Store: s, AllowLive: true, Providers: map[int64]adapter.Provider{7: &autoScopeProvider73{kind: "alipay", account: "seller:2088000000000073", product: "alipay.fund.trans.toaccount.transfer"}}}
	in := SettlementAutoInput{GenerationAuthorizationID: gen.ID, ValidFrom: gen.ValidFrom, ExpiresAt: gen.ExpiresAt, MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 1, CostPolicy: "provider_managed_excluded", Channels: []SettlementAutoChannel{{MerchantID: 7391, ChannelID: 7}}}
	if _, e = g.AuthorizeSettlementAuto(ctx, p.ID, p.Revision, in, settlementPlanAdmin); !errors.Is(e, ErrInvalid) {
		t.Fatal("generation authority also authorized payout", e)
	}
	uses := 0
	guard := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, key string) error {
		if kind != "settlement-auto-authorize" {
			return ErrInvalid
		}
		uses++
		return nil
	})
	if _, e = g.AuthorizeSettlementAuto(guard, p.ID, p.Revision, in, settlementPlanAdmin); !errors.Is(e, ErrState) || uses != 0 {
		t.Fatal("automatic authority lacked original merchant/channel admission", uses, e)
	}
	g.MerchantChannels = map[int64][]int64{7391: {7}}
	a, e := g.AuthorizeSettlementAuto(guard, p.ID, p.Revision, in, settlementPlanAdmin)
	if e != nil || a.ID <= 0 || uses != 1 {
		t.Fatal("independent authority", a, uses, e)
	}
	if _, e = g.AuthorizeSettlementAuto(guard, p.ID, p.Revision, in, settlementPlanAdmin); !errors.Is(e, ErrConflict) || uses != 1 {
		t.Fatal("CAS reused original confirm", uses, e)
	}
	view, e := s.SettlementAutoAuthorization(ctx, p.ID, settlementPlanAdmin)
	if e != nil || view.ID != a.ID || view.Revoked {
		t.Fatal(view, e)
	}
	in.ExpectedAuthorizationID = a.ID
	in.ExpiresAt = in.ExpiresAt.Add(time.Hour)
	if _, e = g.AuthorizeSettlementAuto(guard, p.ID, p.Revision, in, settlementPlanAdmin); !errors.Is(e, ErrState) || uses != 1 {
		t.Fatal("auto authority outlived original generation", uses, e)
	}
	if e = s.RevokeSettlementAuto(ctx, a.ID, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	view, e = s.SettlementAutoAuthorization(ctx, p.ID, settlementPlanAdmin)
	if e != nil || !view.Revoked {
		t.Fatal("revoked authority still armed", view, e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE settlement_auto_authorizations SET proof_hash=decode(repeat('00',32),'hex') WHERE id=$1", a.ID); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SettlementAutoAuthorization(ctx, p.ID, settlementPlanAdmin); !errors.Is(e, ErrConflict) {
		t.Fatal("forged independent source accepted", e)
	}
	check, e := s.CheckLedger(ctx)
	if e != nil || check.InvalidSettlementPlans == 0 {
		t.Fatal("auto authority omitted from checker", check, e)
	}
}
