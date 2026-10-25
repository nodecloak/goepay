package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSettlementPlanGenerateReviewNewAuthorityDoesNotResetDailyBudget(t *testing.T) {
	s := testStore(t)
	ctx := settlementAuthorityReviewGuard(t, s)
	var plan SettlementPlanView
	for _, mid := range []int64{901, 902, 903, 904} {
		ready := settlementPlanReady(t, s, mid)
		if mid == 901 {
			plan = ready
		}
		incomePaidOrder(t, s, mid, fmt.Sprintf("review-day-budget-%d", mid))
	}
	definition := settlementPlanInputFor(901)
	definition.MerchantIDs = []int64{901, 902, 903, 904}
	plan, err := s.UpdateSettlementPlan(ctx, plan.ID, 1, definition, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	in := settlementPlanAuthorizationInput(t, s)
	in.MaxItemGross, in.MaxRunGross, in.MaxDayGross, in.MaxDayItems = 100, 150, 150, 4
	a, err := s.AuthorizeSettlementPlan(ctx, plan.ID, plan.Revision, in, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateSettlementPlanRun(ctx, plan.ID, plan.Revision, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.GenerateSettlementPlanRun(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	// A newly authorized definition can add only the remaining daily amount.
	// Its fresh run/authority counters must not turn the day budget into200+150.
	for round := int64(2); round <= 3; round++ {
		var now time.Time
		if err = s.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			t.Fatal(err)
		}
		definition.Schedule.AnchorAt = now.UTC().Truncate(time.Second).Add(-time.Duration(round) * time.Hour)
		plan, err = s.UpdateSettlementPlan(ctx, plan.ID, plan.Revision, definition, settlementPlanAdmin)
		if err != nil {
			t.Fatal(err)
		}
		in.ExpectedAuthorizationID = a.ID
		in.MaxRunGross, in.MaxDayGross = 100, 200
		a, err = s.AuthorizeSettlementPlan(ctx, plan.ID, plan.Revision, in, settlementPlanAdmin)
		if err != nil {
			t.Fatal(err)
		}
		run, e := s.CreateSettlementPlanRun(ctx, plan.ID, plan.Revision, settlementPlanAdmin)
		if e != nil {
			t.Fatal(e)
		}
		if e = s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
			t.Fatal(e)
		}
		if e = s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
			t.Fatal("same original run replay failed", e)
		}
	}
	var gross, uses, requests int64
	if err = s.pool.QueryRow(ctx, "SELECT (SELECT COALESCE(sum(gross),0) FROM settlement_plan_budget_uses WHERE plan_id=$1),(SELECT count(*) FROM settlement_plan_budget_uses WHERE plan_id=$1),(SELECT count(*) FROM ledger_requests)", plan.ID).Scan(&gross, &uses, &requests); err != nil || gross != 200 || uses != 3 || requests != 3 {
		t.Fatal("new authority reset daily budget or intent", gross, uses, requests, err)
	}
	if b, e := s.Balance(ctx, 904); e != nil || b.Available != 100 || b.Reserved != 0 {
		t.Fatal("exhausted daily budget still reserved fourth wallet", b, e)
	}
	if report, e := s.CheckLedger(ctx); e != nil || report.InvalidSettlementPlans != 0 || report.InvalidSettlementFundings != 0 {
		t.Fatal("later authority invalidated older bounded evidence", report, e)
	}
}

func TestSettlementPlanGenerateReviewExpiryAfterRealAccountLockWait(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	p := settlementPlanReady(t, s, 901)
	incomePaidOrder(t, s, 901, "review-expiry-lock-income")
	guarded := settlementAuthorityReviewGuard(t, s)
	in := settlementPlanAuthorizationInput(t, s)
	var now time.Time
	if err := s.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	in.ExpiresAt = now.Add(2 * time.Second)
	if _, err := s.AuthorizeSettlementPlan(guarded, p.ID, 1, in, settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	var holderPID int32
	if err = holder.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err = holder.Exec(ctx, "SELECT id FROM accounts WHERE owner_id=901 AND kind='available' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.GenerateSettlementPlanRun(ctx, run.ID) }()
	for {
		var blocked int
		if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity a WHERE a.datname=current_database() AND $1=ANY(pg_blocking_pids(a.pid))", holderPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked > 0 {
			break
		}
		select {
		case e := <-done:
			t.Fatal("generation did not reach real original account lock", e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if _, err = holder.Exec(ctx, "SELECT pg_sleep(GREATEST(0,extract(epoch FROM ($1::timestamptz-clock_timestamp())))+0.03)", in.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if err = holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil && !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	var requests, uses, intents int
	if err = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM settlement_plan_budget_uses),(SELECT count(*) FROM settlement_scheduled_intents)").Scan(&requests, &uses, &intents); err != nil || requests != 0 || uses != 0 || intents != 0 {
		t.Fatal("expired while waiting still reserved original money", requests, uses, intents, err)
	}
	if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	view, err := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if err != nil || view.BlockedCount != 1 || len(view.Items) != 1 || view.Items[0].Reason != "authorization_expired" {
		t.Fatal(view, err)
	}
	if b, e := s.Balance(ctx, 901); e != nil || b.Available != 100 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
}

func TestSettlementPlanGenerateReviewSystemActorCannotManufacturePlanSource(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	settlementPlanReady(t, s, 901)
	incomePaidOrder(t, s, 901, "review-system-source-income")
	_, err := s.RequestFunds(ctx, "settlements", 901, 0, "unsealed-system-source", 10, "system:settlement-scheduler")
	var guardError *pgconn.PgError
	if !errors.As(err, &guardError) || guardError.Code != "P0001" || guardError.Message != "missing scheduled request source" {
		t.Fatal("system actor did not require a real original plan source", err)
	}
	if b, e := s.Balance(ctx, 901); e != nil || b.Available != 100 || b.Reserved != 0 {
		t.Fatal("rejected source left money reserved", b, e)
	}
	manual, err := s.RequestFunds(ctx, "settlements", 901, 0, "ordinary-manual-source", 10, "merchant:901")
	if err != nil || manual.Status != "pending" {
		t.Fatal("scheduling source guard intruded into ordinary manual flow", manual, err)
	}
	if report, e := s.CheckLedger(ctx); e != nil || report.InvalidSettlementPlans != 0 || report.InvalidSettlementFundings != 0 {
		t.Fatal(report, e)
	}
}

func TestSettlementPlanGenerateReviewUsesOriginalD1FeeAndRetention(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 901)
	old := incomePaidOrder(t, s, 901, "review-d1-old-income")
	incomePaidOrder(t, s, 901, "review-d1-today-income")
	if _, err := s.pool.Exec(ctx, "UPDATE orders SET paid_at=clock_timestamp()-interval '2 days' WHERE id=$1", old.ID); err != nil {
		t.Fatal(err)
	}
	transferSetting(t, s, "settle_type", "1")
	transferSetting(t, s, "settlement_retained_cents", "10")
	transferSetting(t, s, "settlement_fee_rate_bps", "250")
	if _, err := s.AuthorizeSettlementPlan(settlementAuthorityReviewGuard(t, s), p.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	view, err := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if err != nil || len(view.Items) != 1 {
		t.Fatal(view, err)
	}
	item := view.Items[0]
	if item.State != "created" || item.LedgerAvailable != 200 || item.Eligible != 100 || item.Retained != 10 || item.Gross != 90 || item.Fee != 2 || item.Net != 88 {
		t.Fatal("scheduled quote relaxed D1, deducted retention twice, or changed original fee rounding", item)
	}
	funds, err := s.FundsAvailability(ctx, 901)
	if err != nil || funds.LedgerAvailable != 110 || funds.TodayGross != 100 || funds.Available != 10 {
		t.Fatal("scheduled reservation consumed today's blocked income or retained funds", funds, err)
	}
}

func TestSettlementPlanGenerateReviewRechecksRecipientPreferenceAndFeeScope(t *testing.T) {
	for _, change := range []string{"recipient", "preference_off", "preference_revision", "fee"} {
		t.Run(change, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			p := settlementPlanReady(t, s, 901)
			incomePaidOrder(t, s, 901, "review-scope-income")
			if _, err := s.AuthorizeSettlementPlan(settlementAuthorityReviewGuard(t, s), p.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); err != nil {
				t.Fatal(err)
			}
			run, err := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
			if err != nil {
				t.Fatal(err)
			}
			owner := SettlementPlanActor{Role: "merchant", ID: 901, Name: "local-owner"}
			pref, err := s.SettlementSchedulePreference(ctx, 901, owner)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "recipient":
				_, err = s.SetSettlementRecipient(ctx, 901, SettlementRecipientInput{Type: "alipay", Account: "new-local-recipient@example.test", Name: "Changed Local Recipient"}, &pref.RecipientConfigID, SettlementRecipientActor{Role: "merchant", ID: 901, Name: owner.Name})
			case "preference_off":
				_, err = s.SetSettlementSchedulePreference(ctx, 901, 1, false, 0, owner)
			case "preference_revision":
				_, err = s.SetSettlementSchedulePreference(ctx, 901, 1, true, pref.RecipientConfigID, owner)
			case "fee":
				transferSetting(t, s, "settlement_fee_rate_bps", "250")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			view, err := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
			if err != nil || len(view.Items) != 1 || view.Items[0].State != "blocked" || view.Items[0].Reason != "scope_changed" {
				t.Fatal("changed scope reused original authority", view, err)
			}
			if b, e := s.Balance(ctx, 901); e != nil || b.Available != 100 || b.Reserved != 0 {
				t.Fatal("changed scope reserved original money", b, e)
			}
		})
	}
}

func TestSettlementPlanGenerateReviewCheckerCountsParentsWhenProjectionsDisappear(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 901)
	incomePaidOrder(t, s, 901, "review-parent-projections-income")
	if _, err := s.AuthorizeSettlementPlan(settlementAuthorityReviewGuard(t, s), p.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	views := []struct{ name, parent string }{
		{"settlement_plan_head_invariants", "settlement_plans"},
		{"settlement_plan_definition_invariants", "settlement_plan_versions"},
		{"settlement_plan_preference_invariants", "settlement_schedule_preferences"},
		{"settlement_plan_authorization_invariants", "settlement_plan_authorizations"},
		{"settlement_plan_run_invariants", "settlement_plan_runs"},
		{"settlement_plan_item_invariants", "settlement_plan_items"},
	}
	for _, view := range views {
		var original string
		if err = s.pool.QueryRow(ctx, "SELECT pg_get_viewdef($1::regclass,true)", view.name).Scan(&original); err != nil {
			t.Fatal(err)
		}
		// This independent random schema deliberately models a damaged restore
		// with a missing projection. No business row, HMAC or guard is changed.
		if _, err = s.pool.Exec(ctx, "CREATE OR REPLACE VIEW "+view.name+" AS SELECT id,false AS valid FROM "+view.parent+" WHERE false"); err != nil {
			t.Fatal(err)
		}
		report, checkErr := s.CheckLedger(ctx)
		if _, err = s.pool.Exec(ctx, "CREATE OR REPLACE VIEW "+view.name+" AS "+original); err != nil {
			t.Fatal(err)
		}
		if checkErr != nil || report.InvalidSettlementPlans < 1 {
			t.Fatal("missing projection erased its original parent from ledger check", view.name, report, checkErr)
		}
	}
	if report, e := s.CheckLedger(ctx); e != nil || report.InvalidSettlementPlans != 0 || report.InvalidSettlementFundings != 0 {
		t.Fatal("restoring original projections left unexplained corruption", report, e)
	}
}

func TestSettlementPlanGenerateReviewReviewCannotAdoptUnsignedRestoredItem(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 901)
	incomePaidOrder(t, s, 901, "review-restored-item-income")
	if _, err := s.AuthorizeSettlementPlan(settlementAuthorityReviewGuard(t, s), p.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	view, err := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if err != nil || len(view.Items) != 1 || view.Items[0].RequestID <= 0 {
		t.Fatal(view, err)
	}
	item := view.Items[0]
	// Explicit damaged-restore fixture: lose only the immutable item trigger,
	// replace its signature with32zero bytes, then restore the trigger. All
	// source, ledger, recipient, audit and budget records remain genuine.
	if _, err = s.pool.Exec(ctx, "ALTER TABLE settlement_plan_items DISABLE TRIGGER settlement_plan_item_immutable"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, "UPDATE settlement_plan_items SET proof_hash=decode(repeat('00',32),'hex') WHERE id=$1", item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, "ALTER TABLE settlement_plan_items ENABLE TRIGGER settlement_plan_item_immutable"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin); !errors.Is(err, ErrConflict) {
		t.Fatal("damaged item did not reach signature boundary", err)
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidSettlementPlans < 1 {
		t.Fatal("checker adopted unsigned restored item", report, err)
	}
	if _, e := s.SettlementPayout(ctx, item.RequestID); !errors.Is(e, ErrConflict) {
		t.Error("payout read ignored unsigned original source before looking up payout", e)
	}
	payoutTx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, lockErr := s.lockSettlementRequest(ctx, payoutTx, item.RequestID)
	rollbackErr := payoutTx.Rollback(ctx)
	if !errors.Is(lockErr, ErrConflict) || rollbackErr != nil {
		t.Error("payout mutation lock adopted unsigned scheduled original source", lockErr, rollbackErr)
	}
	if replay, e := s.RequestFunds(ctx, "settlements", 901, 0, fmt.Sprintf("SP%d", item.ID), item.Gross, "merchant:901"); !errors.Is(e, ErrConflict) {
		t.Error("ordinary request replay adopted unsigned scheduled original source", replay, e)
	}
	request, err := s.ReviewFundsRequest(ctx, item.RequestID, true, "admin:1")
	if !errors.Is(err, ErrConflict) {
		t.Fatal("manual review adopted unsigned scheduled original source", request, err)
	}
	if request, err = s.ReviewFundsRequest(ctx, item.RequestID, false, "admin:1"); !errors.Is(err, ErrConflict) {
		t.Fatal("manual rejection adopted unsigned scheduled original source", request, err)
	}
	var state string
	if err = s.pool.QueryRow(ctx, "SELECT status FROM ledger_requests WHERE id=$1", item.RequestID).Scan(&state); err != nil || state != "pending" {
		t.Fatal("unsigned source changed financial review state", state, err)
	}
	if b, e := s.Balance(ctx, 901); e != nil || b.Available != 0 || b.Reserved != 100 {
		t.Fatal("unsigned source changed financial balances", b, e)
	}
}

func TestSettlementPlanGenerateReviewAuditRollbackAndHistoricalManualReview(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 901)
	incomePaidOrder(t, s, 901, "review-generation-audit-income")
	a, err := s.AuthorizeSettlementPlan(settlementAuthorityReviewGuard(t, s), p.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `CREATE FUNCTION local_generation_audit_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='settlement-plan-items' THEN RAISE EXCEPTION 'local generation audit failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER local_generation_audit_fail BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION local_generation_audit_fail()`); err != nil {
		t.Fatal(err)
	}
	if err = s.GenerateSettlementPlanRun(ctx, run.ID); err == nil {
		t.Fatal("generation ignored original item audit failure")
	}
	var requests, fundings, reserves, items, uses, intents int
	if err = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM settlement_fundings),(SELECT count(*) FROM accounts WHERE kind='settlement_reserve'),(SELECT count(*) FROM settlement_plan_items),(SELECT count(*) FROM settlement_plan_budget_uses),(SELECT count(*) FROM settlement_scheduled_intents)`).Scan(&requests, &fundings, &reserves, &items, &uses, &intents); err != nil || requests+fundings+reserves+items+uses+intents != 0 {
		t.Fatal("item audit failure left partial original intent or reserve", requests, fundings, reserves, items, uses, intents, err)
	}
	if b, e := s.Balance(ctx, 901); e != nil || b.Available != 100 || b.Reserved != 0 {
		t.Fatal("failed generation changed wallet", b, e)
	}
	if _, err = s.pool.Exec(ctx, "DROP TRIGGER local_generation_audit_fail ON backoffice_audit"); err != nil {
		t.Fatal(err)
	}
	if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil {
		t.Fatal("same original run could not recover rolled-back generation", err)
	}
	view, err := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if err != nil || len(view.Items) != 1 || view.Items[0].State != "created" {
		t.Fatal(view, err)
	}
	if err = s.RevokeSettlementPlanAuthorization(ctx, a.ID, settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetSettlementSchedulePreference(ctx, 901, 1, false, 0, SettlementPlanActor{Role: "merchant", ID: 901, Name: "local-owner"}); err != nil {
		t.Fatal(err)
	}
	// Revocation prevents new intent creation; it does not invalidate a proven
	// original reserve that is already waiting for a separate human decision.
	if request, e := s.ReviewFundsRequest(ctx, view.Items[0].RequestID, true, "admin:1"); e != nil || request.Status != "approved" {
		t.Fatal("historical authentic request became impossible to review after revocation", request, e)
	}
	if report, e := s.CheckLedger(ctx); e != nil || report.InvalidSettlementPlans != 0 || report.InvalidSettlementFundings != 0 {
		t.Fatal(report, e)
	}
}

func TestSettlementPlanGenerateReviewMaturityChangeDuringAccountWaitCannotBroadenScope(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := settlementPlanReady(t, s, 901)
	incomePaidOrder(t, s, 901, "review-maturity-lock-income")
	transferSetting(t, s, "settle_type", "1")
	if _, err := s.AuthorizeSettlementPlan(settlementAuthorityReviewGuard(t, s), p.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	var holderPID int32
	if err = holder.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err = holder.Exec(ctx, "SELECT id FROM accounts WHERE owner_id=901 AND kind='available' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.GenerateSettlementPlanRun(ctx, run.ID) }()
	for {
		var blocked int
		if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity a WHERE a.datname=current_database() AND $1=ANY(pg_blocking_pids(a.pid))", holderPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked > 0 {
			break
		}
		select {
		case e := <-done:
			t.Fatal("generation never reached original account wait", e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	// The already-authorized scope is D1. While generation waits for the
	// original account, a legitimate global change must not silently let this
	// old authority reserve today's previously ineligible income.
	if _, err = holder.Exec(ctx, "SET LOCAL lock_timeout='150ms'"); err != nil {
		t.Fatal(err)
	}
	_, changeErr := holder.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"0"'::jsonb) WHERE resource='settings' AND body->>'key'='settle_type'`)
	if changeErr == nil {
		if err = holder.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	} else {
		var lockError *pgconn.PgError
		if !errors.As(changeErr, &lockError) || lockError.Code != "55P03" {
			t.Fatal("maturity mutation failed for an unrelated reason", changeErr)
		}
		if err = holder.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err = <-done; err != nil && !errors.Is(err, ErrConflict) && !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	var requests, uses int
	if err = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM settlement_plan_budget_uses)").Scan(&requests, &uses); err != nil || requests != 0 || uses != 0 {
		t.Fatal("D1 authority silently adopted D0 scope after waiting on account", requests, uses, err)
	}
	if b, e := s.Balance(ctx, 901); e != nil || b.Available != 100 || b.Reserved != 0 {
		t.Fatal("changed maturity scope consumed today's income", b, e)
	}
}

func TestSettlementPlanGenerateReviewLateRecipientConflictCannotLeaveOrphanReserve(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 901)
	incomePaidOrder(t, s, 901, "review-late-recipient-income")
	if _, err := s.AuthorizeSettlementPlan(settlementAuthorityReviewGuard(t, s), p.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	// Local fault injection at a real late fallible step: the helper has already
	// inserted its original request/reserve/terms when recipient snapshotting
	// discovers a missing current configuration. No guard is disabled.
	if _, err = s.pool.Exec(ctx, `CREATE FUNCTION local_late_recipient_conflict() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN DELETE FROM settlement_recipient_current WHERE merchant_id=(SELECT merchant_id FROM ledger_requests WHERE id=NEW.request_id); RETURN NEW; END $$;
CREATE TRIGGER local_late_recipient_conflict AFTER INSERT ON settlement_request_terms FOR EACH ROW EXECUTE FUNCTION local_late_recipient_conflict()`); err != nil {
		t.Fatal(err)
	}
	if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil && !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	var requests, reserves, fundings int
	if err = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM accounts WHERE kind='settlement_reserve'),(SELECT count(*) FROM settlement_fundings)").Scan(&requests, &reserves, &fundings); err != nil || requests != 0 || reserves != 0 || fundings != 0 {
		t.Fatal("late helper conflict committed an orphan reserve as a blocked plan item", requests, reserves, fundings, err)
	}
	if b, e := s.Balance(ctx, 901); e != nil || b.Available != 100 || b.Reserved != 0 {
		t.Fatal("late recipient conflict leaked original funds", b, e)
	}
	var configs int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM settlement_recipient_current WHERE merchant_id=901").Scan(&configs); err != nil || configs != 1 {
		t.Fatal("failed helper's local mutation survived rollback", configs, err)
	}
}
