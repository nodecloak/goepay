package payment

import (
	"context"
	"sync"
	"testing"
)

func TestSettlementPlanGenerateReservesOriginalIntentOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 901)
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 901, OutTradeNo: "schedule-income", Name: "local", Amount: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkPaid(ctx, o.TradeNo, "local-schedule-receipt"); err != nil {
		t.Fatal(err)
	}
	guarded := settlementPlanGuard(t, s)
	if _, err = s.AuthorizeSettlementPlan(guarded, p.ID, p.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if err != nil {
		t.Fatal("create due original run", err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	view, err := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if err != nil || view.CreatedCount != 1 || view.State != "completed" || len(view.Items) != 1 || view.Items[0].Gross != 1000 || view.Items[0].Net != 1000 || view.Items[0].State != "created" {
		t.Fatal("original pending request missing", view, err)
	}
	var requests, intents, uses, grants, jobs int
	if err = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM settlement_scheduled_intents),(SELECT count(*) FROM settlement_plan_budget_uses),(SELECT count(*) FROM local_plan_grants),(SELECT count(*) FROM jobs WHERE kind='settlement-plan.generate')`).Scan(&requests, &intents, &uses, &grants, &jobs); err != nil || requests != 1 || intents != 1 || uses != 1 || grants != 1 || jobs != 1 {
		t.Fatal("duplicate reserve, grant or job", requests, intents, uses, grants, jobs, err)
	}
	b, err := s.Balance(ctx, 901)
	if err != nil || b.Available != 1000 || b.Reserved != 1000 {
		t.Fatal(b, err)
	}
	if again, e := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin); e != nil || again.ID != run.ID {
		t.Fatal("due scan changed original run", again, e)
	}
	requestID := view.Items[0].RequestID
	if _, err = s.ReviewFundsRequest(ctx, requestID, false, "admin:1"); err != nil {
		t.Fatal("original request rejection", err)
	}
	// A different plan and a restored wallet cannot re-arm today's intent.
	second, err := s.CreateSettlementPlan(ctx, settlementPlanInputFor(901), settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AuthorizeSettlementPlan(guarded, second.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateSettlementPlanRun(ctx, second.ID, 1, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.GenerateSettlementPlanRun(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	other, err = s.SettlementPlanRun(ctx, other.ID, settlementPlanAdmin)
	if err != nil || other.CreatedCount != 0 || other.SkippedCount != 1 || len(other.Items) != 1 || other.Items[0].Reason != "already_scheduled" {
		t.Fatal("overlapping plan re-armed merchant day", other, err)
	}
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_requests WHERE merchant_id=901").Scan(&requests); err != nil || requests != 1 {
		t.Fatal(requests, err)
	}
	if b, err = s.Balance(ctx, 901); err != nil || b.Available != 2000 || b.Reserved != 0 {
		t.Fatal(b, err)
	}
}

func TestSettlementPlanGenerateRecordsDailyLimitWithoutRetryLoop(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 901)
	if _, err := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settlement_daily_limit","value":"1"}')`); err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 901, OutTradeNo: "daily-limit-income", Name: "local", Amount: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkPaid(ctx, o.TradeNo, "local-daily-receipt"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RequestFunds(ctx, "settlements", 901, 0, "manual-first", 100, "merchant:901"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil {
		t.Fatal("daily quota should become a fixed skip, not a retry loop", err)
	}
	v, err := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if err != nil || v.SkippedCount != 1 || len(v.Items) != 1 || v.Items[0].Reason != "daily_limit" {
		t.Fatal(v, err)
	}
	var requests int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_requests WHERE merchant_id=901").Scan(&requests); err != nil || requests != 1 {
		t.Fatal("quota skip created a second request", requests, err)
	}
}

func TestSettlementPlanGenerateStopsChangedScopeAndRevokedAuthority(t *testing.T) {
	for _, reason := range []string{"scope_changed", "authorization_revoked"} {
		t.Run(reason, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			p := settlementPlanReady(t, s, 901)
			incomePaidOrder(t, s, 901, "scope-money")
			a, err := s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin)
			if err != nil {
				t.Fatal(err)
			}
			run, err := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
			if err != nil {
				t.Fatal(err)
			}
			if reason == "scope_changed" {
				_, err = s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settlement_retained_cents","value":"1"}')`)
			} else {
				err = s.RevokeSettlementPlanAuthorization(ctx, a.ID, settlementPlanAdmin)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			v, err := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
			if err != nil || v.BlockedCount != 1 || v.Items[0].Reason != reason {
				t.Fatal(v, err)
			}
			var n int
			if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_requests").Scan(&n); err != nil || n != 0 {
				t.Fatal("invalid authority reserved funds", n, err)
			}
		})
	}
}
