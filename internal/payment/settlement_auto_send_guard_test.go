package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

func autoReady73(t *testing.T) (*Store, ProviderFlow, *autoScopeProvider73, SettlementPlanView, SettlementAutoAuthorizationView, int64) {
	t.Helper()
	return autoReadyStore73(t, testStore(t))
}

func autoReadyStore73(t *testing.T, s *Store) (*Store, ProviderFlow, *autoScopeProvider73, SettlementPlanView, SettlementAutoAuthorizationView, int64) {
	t.Helper()
	ctx := context.Background()
	p := settlementPlanReady(t, s, 7395)
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{transfer_api_enabled}','true') WHERE id=7395`); e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "transfer_enabled", "true")
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7395, OutTradeNo: "auto73-guard-credit", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "auto73-guard-local"); e != nil {
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
	v, e := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if e != nil || len(v.Items) != 1 || v.Items[0].State != "created" {
		t.Fatal(v, e)
	}
	provider := &autoScopeProvider73{kind: "alipay", account: "seller:2088000000000073", product: "alipay.fund.trans.toaccount.transfer"}
	provider.unknown = true
	g := ProviderFlow{Store: s, AllowLive: true, MerchantChannels: map[int64][]int64{7395: {7}}, Providers: map[int64]adapter.Provider{7: provider}}
	guard := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, _ string) error {
		if kind != "settlement-auto-authorize" {
			return ErrInvalid
		}
		return nil
	})
	a, e := g.AuthorizeSettlementAuto(guard, p.ID, p.Revision, SettlementAutoInput{GenerationAuthorizationID: gen.ID, ValidFrom: gen.ValidFrom, ExpiresAt: gen.ExpiresAt, MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 1, CostPolicy: "provider_managed_excluded", Channels: []SettlementAutoChannel{{MerchantID: 7395, ChannelID: 7}}}, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	return s, g, provider, p, a, v.Items[0].RequestID
}

func TestSettlementAuto73RevokedDuringFinalDatabaseWaitNeverSends(t *testing.T) {
	s, g, provider, _, a, id := autoReady73(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var blocker pgx.Tx
	defer func() {
		if blocker != nil {
			_ = blocker.Rollback(context.Background())
		}
	}()
	finished := make(chan error, 1)
	checks := 0
	check := func(c context.Context, _ int64) error {
		checks++
		if checks != 3 {
			return nil
		}
		var e error
		blocker, e = s.pool.Begin(c)
		if e != nil {
			return e
		}
		var pid int64
		if e = blocker.QueryRow(c, "SELECT pg_backend_pid() FROM merchants WHERE id=7395 FOR NO KEY UPDATE").Scan(&pid); e != nil {
			return e
		}
		go func() {
			for {
				var waiting bool
				e := s.pool.QueryRow(c, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)))", pid).Scan(&waiting)
				if e != nil {
					finished <- e
					return
				}
				if waiting {
					break
				}
				select {
				case <-c.Done():
					finished <- c.Err()
					return
				case <-time.After(5 * time.Millisecond):
				}
			}
			e := s.RevokeSettlementAuto(c, a.ID, settlementPlanAdmin)
			if e == nil {
				e = blocker.Commit(c)
			}
			finished <- e
		}()
		return nil
	}
	out, e := g.ExecuteSettlementAuto(ctx, id, check)
	if err := <-finished; err != nil {
		t.Fatal("local revocation during confirmed database wait", err)
	}
	if !errors.Is(e, ErrState) || provider.transfers.Load() != 0 || out.SendState != "claimed" {
		t.Fatal("revoked authority crossed final database wait", out.SendState, provider.transfers.Load(), e)
	}
	if _, e = g.ExecuteSettlementAuto(ctx, id, nil); e != nil || provider.transfers.Load() != 0 {
		t.Fatal("stopped claim rearmed", e)
	}
	var claims int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM settlement_auto_claims WHERE request_id=$1", id).Scan(&claims); e != nil || claims != 1 {
		t.Fatal(claims, e)
	}
	b, e := s.Balance(ctx, 7395)
	if e != nil || b.Available != 1000 || b.Reserved != 1000 {
		t.Fatal("uncertain send barrier lost original reserve", b, e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidSettlementPlans != 0 || report.InvalidSettlementFundings != 0 {
		t.Fatal(fmt.Sprint(report), e)
	}
}

func TestSettlementAuto73RevocationWaitingForClaimUsesActualLockOrderTime(t *testing.T) {
	for _, generation := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "generation"}[generation], func(t *testing.T) {
			s, g, _, _, a, id := autoReady73(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			started := false
			check := func(c context.Context, _ int64) error {
				if started {
					return nil
				}
				started = true
				go func() {
					if generation {
						finished <- s.RevokeSettlementPlanAuthorization(c, a.GenerationAuthorizationID, settlementPlanAdmin)
					} else {
						finished <- s.RevokeSettlementAuto(c, a.ID, settlementPlanAdmin)
					}
				}()
				for {
					var waiting bool
					e := s.pool.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query='SELECT id FROM settlement_plans WHERE id=$1 FOR UPDATE')`).Scan(&waiting)
					if e != nil {
						return e
					}
					if waiting {
						return nil
					}
					select {
					case <-c.Done():
						return c.Err()
					case <-time.After(5 * time.Millisecond):
					}
				}
			}
			out, e := g.ExecuteSettlementAuto(ctx, id, check)
			var unknown *adapter.UnknownOutcome
			if e != nil && !errors.Is(e, ErrState) && !errors.As(e, &unknown) {
				t.Fatal("unexpected first claim result", e)
			}
			if out.SendState != "claimed" {
				t.Fatal("fixture never committed original claim", out.SendState, e)
			}
			if e = <-finished; e != nil {
				t.Fatal(e)
			}
			table, aid := "settlement_auto_revocations", a.ID
			if generation {
				table, aid = "settlement_plan_revocations", a.GenerationAuthorizationID
			}
			var ordered bool
			if e = s.pool.QueryRow(ctx, "SELECT r.created_at>=c.claimed_at FROM "+table+" r CROSS JOIN settlement_auto_claims c WHERE r.authorization_id=$1 AND c.request_id=$2", aid, id).Scan(&ordered); e != nil || !ordered {
				t.Fatal("waiting revoke was backdated before an already committed claim", ordered, e)
			}
			report, e := s.CheckLedger(ctx)
			if e != nil || report.InvalidSettlementPlans != 0 || report.InvalidSettlementFundings != 0 {
				t.Fatal("legitimate earlier claim lost recoverable source", report, e)
			}
		})
	}
}
