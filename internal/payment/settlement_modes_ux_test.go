package payment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestSettlementModeUXThresholdAcceptsOriginalBothAndInheritance(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 11801, Name: "结算模式本地商户", APIKey: "settlement-mode-synthetic-only"}); err != nil {
		t.Fatal(err)
	}
	var gid int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"原模式","settle_open":"3"}') RETURNING id`).Scan(&gid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=11801`, gid); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []any{"3", json.Number("3"), "_", "", json.Number("0"), "1", "2"} {
		raw, _ := json.Marshal(map[string]any{"name": "原模式", "settle_open": mode})
		if _, err := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=$2 WHERE id=$1`, gid, raw); err != nil {
			t.Fatal(err)
		}
		policy, err := readSettlementThresholdMerchantPolicy(ctx, s.pool, 11801, SettlementThresholdPolicy{ThresholdCents: 100}, time.Now())
		if err != nil || policy.GroupDisabled != (mode == "2") {
			t.Fatalf("original threshold mode %#v wrong: disabled=%v err=%v", mode, policy.GroupDisabled, err)
		}
	}
}

func TestSettlementModeUXManualRejectsAutoOnlyBeforeReservingAndPreservesExistingReplay(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 11802, Name: "手动模式本地商户", APIKey: "settlement-manual-synthetic-only"}); err != nil {
		t.Fatal(err)
	}
	order, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 11802, OutTradeNo: "settlement-mode-funds", Amount: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkPaid(ctx, order.TradeNo, "settlement-mode-paid"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetSettlementRecipient(ctx, 11802, SettlementRecipientInput{Type: "alipay", Account: "mode-payee@example.invalid", Name: "本地收款人"}, nil, SettlementRecipientActor{Role: "merchant", ID: 11802, Name: "local"}); err != nil {
		t.Fatal(err)
	}
	original, err := s.RequestFunds(ctx, "settlements", 11802, 0, "before-mode-change", 100, "merchant:11802")
	if err != nil {
		t.Fatal(err)
	}
	var gid int64
	if err = s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"仅自动组","settle_open":"1"}') RETURNING id`).Scan(&gid); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=11802`, gid); err != nil {
		t.Fatal(err)
	}
	before, err := s.Balance(ctx, 11802)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RequestFunds(ctx, "settlements", 11802, 0, "auto-only-manual-denied", 100, "merchant:11802"); !errors.Is(err, ErrState) {
		t.Fatalf("auto-only group created a new manual reserve: %v", err)
	}
	if _, err = s.RequestFunds(ctx, "settlements", 11802, 0, "forged-auto-actor", 100, "system:settlement-plan:1"); !errors.Is(err, ErrSettlementMode) {
		t.Fatalf("actor text bypassed manual-only origin: %v", err)
	}
	after, err := s.Balance(ctx, 11802)
	if err != nil || before != after {
		t.Fatal("denied manual attempt changed balances")
	}
	replay, err := s.RequestFunds(ctx, "settlements", 11802, 0, "before-mode-change", 100, "merchant:11802")
	if err != nil || replay.ID != original.ID {
		t.Fatalf("business mode blocked original intent lookup: %v", err)
	}
}

func TestSettlementModeUXGlobalInheritanceOverridesAndInvalidValues(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 11804, Name: "本地模式矩阵", APIKey: "mode-matrix-local-only"}); err != nil {
		t.Fatal(err)
	}
	var global, group int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settle_open","value":"0"}') RETURNING id`).Scan(&global); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"本地模式组"}') RETURNING id`).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1,profile='{"settle_open":"3"}' WHERE id=11804`, group); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"0", "1", "2", "3"} {
		raw, _ := json.Marshal(map[string]any{"key": "settle_open", "value": mode})
		if _, err := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=$2 WHERE id=$1`, global, raw); err != nil {
			t.Fatal(err)
		}
		for _, inherit := range []string{"", "_", "0"} {
			body, _ := json.Marshal(map[string]any{"name": "本地模式组", "settle_open": inherit})
			if _, err := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=$2 WHERE id=$1`, group, body); err != nil {
				t.Fatal(err)
			}
			got, err := readSettlementAccess(ctx, s.pool, 11804, false, false)
			if err != nil || got.Mode != mode || got.ManualAllowed != (mode == "2" || mode == "3") || got.AutomaticAllowed != (mode == "1" || mode == "3") || got.Source != "global" {
				t.Fatalf("global %s inherit %q: %+v %v", mode, inherit, got, err)
			}
			threshold, err := readSettlementAccess(ctx, s.pool, 11804, true, false)
			if err != nil || !threshold.AutomaticAllowed {
				t.Fatalf("ordinary global mode gated independent threshold: %+v %v", threshold, err)
			}
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"broken"') WHERE id=$1`, global); err != nil {
		t.Fatal(err)
	}
	if _, err := readSettlementAccess(ctx, s.pool, 11804, false, false); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad inherited global accepted", err)
	}
	for _, mode := range []string{"1", "2", "3"} {
		body, _ := json.Marshal(map[string]any{"name": "本地模式组", "settle_open": mode})
		if _, err := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=$2 WHERE id=$1`, group, body); err != nil {
			t.Fatal(err)
		}
		got, err := readSettlementAccess(ctx, s.pool, 11804, false, false)
		if err != nil || got.Mode != mode || got.Source != "group" {
			t.Fatalf("valid override consulted unrelated global: %+v %v", got, err)
		}
	}
	for _, invalid := range []any{"4", "01", true, map[string]string{"mode": "3"}} {
		body, _ := json.Marshal(map[string]any{"name": "本地模式组", "settle_open": invalid})
		if _, err := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=$2 WHERE id=$1`, group, body); err != nil {
			t.Fatal(err)
		}
		if _, err := readSettlementAccess(ctx, s.pool, 11804, false, false); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad group %#v accepted: %v", invalid, err)
		}
	}
}

func TestSettlementModeUXGenerationSkipsNewReserveAfterGlobalModeChanges(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 11805)
	incomePaidOrder(t, s, 11805, "mode-change-before-generation")
	if _, err := s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, 1, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settle_open","value":"2"}')`); err != nil {
		t.Fatal(err)
	}
	if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	view, err := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if err != nil || view.SkippedCount != 1 || len(view.Items) != 1 || view.Items[0].Reason != "settlement_mode" {
		t.Fatalf("mode skip wrong: %+v %v", view, err)
	}
	var requests, intents, uses int
	if err = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM settlement_scheduled_intents),(SELECT count(*) FROM settlement_plan_budget_uses)`).Scan(&requests, &intents, &uses); err != nil || requests+intents+uses != 0 {
		t.Fatalf("mode skip reserved funds: %d %d %d %v", requests, intents, uses, err)
	}
	if b, e := s.Balance(ctx, 11805); e != nil || b.Available != 100 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
}

func TestSettlementModeUXExpiredMembershipCannotReserveAfterConfirmationWait(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 11806, Name: "模式到期本地商户", APIKey: "mode-expiry-local-only"}); err != nil {
		t.Fatal(err)
	}
	incomePaidOrder(t, s, 11806, "membership-mode-expiry-income")
	if _, err := s.SetSettlementRecipient(ctx, 11806, SettlementRecipientInput{Type: "alipay", Account: "expiry@example.invalid", Name: "本地收款人"}, nil, SettlementRecipientActor{Role: "merchant", ID: 11806, Name: "local"}); err != nil {
		t.Fatal(err)
	}
	var base, premium int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"base-auto","settle_open":"1"}') RETURNING id`).Scan(&base); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"premium-manual","enabled":true,"settle_open":"2","price_cents":"0","duration_days":"30"}') RETURNING id`).Scan(&premium); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=11806`, base); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PurchaseMembership(ctx, 11806, premium, "mode-expiry-membership", "merchant:11806"); err != nil {
		t.Fatal(err)
	}
	var expires time.Time
	if err := s.pool.QueryRow(ctx, `UPDATE memberships SET expires_at=clock_timestamp()+interval '1 second' WHERE merchant_id=11806 RETURNING expires_at`).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	quote, err := s.SettlementQuote(ctx, 11806, 100)
	if err != nil || !quote.CanApply || quote.Access.GroupID != premium {
		t.Fatalf("active member lost manual permission: %+v %v", quote, err)
	}
	guard := WithFundsActionGuard(ctx, func(ctx context.Context, tx pgx.Tx, _, _ string) error {
		_, e := tx.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM ($1::timestamptz-clock_timestamp())))+0.03)`, expires)
		return e
	})
	if _, err = s.RequestFunds(guard, "settlements", 11806, 0, "mode-expired-during-confirmation", 100, "merchant:11806"); !errors.Is(err, ErrSettlementMode) {
		t.Fatalf("expired member still reserved: %v", err)
	}
	quote, err = s.SettlementQuote(ctx, 11806, 100)
	if err != nil || quote.CanApply || quote.Access.GroupID != base {
		t.Fatalf("expired membership failed to restore base: %+v %v", quote, err)
	}
	if b, e := s.Balance(ctx, 11806); e != nil || b.Available != 100 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
}

func TestSettlementModeUXNewAutomaticAuthorityRejectsManualOnlyWithoutChangingHistory(t *testing.T) {
	s := testStore(t)
	ctx := settlementPlanGuard(t, s)
	p := settlementPlanReady(t, s, 11803)
	in := settlementPlanAuthorizationInput(t, s)
	a, err := s.AuthorizeSettlementPlan(ctx, p.ID, 1, in, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settle_open","value":"2"}')`); err != nil {
		t.Fatal(err)
	}
	in.ExpectedAuthorizationID = a.ID
	if _, err = s.AuthorizeSettlementPlan(ctx, p.ID, 1, in, settlementPlanAdmin); !errors.Is(err, ErrSettlementMode) {
		t.Fatalf("manual-only system issued fresh automatic authority: %v", err)
	}
	history, err := s.readSettlementPlanAuthorization(ctx, s.pool, a.ID)
	if err != nil || history.View.ID != a.ID {
		t.Fatalf("mode change invalidated sealed history: %v", err)
	}
	var auths, grants, requests int
	if err = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM settlement_plan_authorizations),(SELECT count(*) FROM local_plan_grants),(SELECT count(*) FROM ledger_requests)`).Scan(&auths, &grants, &requests); err != nil || auths != 1 || grants != 1 || requests != 0 {
		t.Fatalf("denied new authority left writes: %d %d %d %v", auths, grants, requests, err)
	}
}
