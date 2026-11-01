package payment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func setGroupSettlementCycle(t *testing.T, s *Store, id int64, value string) {
	t.Helper()
	if _, err := s.pool.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_set(body,'{settle_type}',to_jsonb($2::text)) WHERE resource='groups' AND id=$1`, id, value); err != nil {
		t.Fatal(err)
	}
}

func TestOriginalGroupSettlementCycleControlsOutgoingQuotesAndOriginalReplay(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	setGroupSettlementCycle(t, s, 0, "1")
	quote, err := s.SettlementQuote(ctx, 681, 100)
	if err != nil || quote.Funds.SettleType != 1 || quote.Funds.Available != 0 || quote.CanApply {
		t.Fatal("group D+1 did not restrict quote under global D+0", quote, err)
	}
	if _, err := s.ReserveTransfer(ctx, 681, "group-cycle-transfer", 1); !errors.Is(err, ErrInsufficient) {
		t.Fatal("transfer bypassed group D+1", err)
	}
	for _, kind := range []string{"settlements", "internal-transfers", "profitsharing"} {
		receiver := int64(682)
		if kind == "settlements" {
			receiver = 0
		}
		if _, err := s.RequestFunds(ctx, kind, 681, receiver, kind, 1, "merchant:681"); !errors.Is(err, ErrInsufficient) {
			t.Fatal("outgoing reservation bypassed group D+1", kind, err)
		}
	}
	transferSetting(t, s, "settle_type", "1")
	setGroupSettlementCycle(t, s, 0, "0")
	if quote, err := s.SettlementQuote(ctx, 681, 100); err != nil || quote.Funds.SettleType != 0 || !quote.CanApply {
		t.Fatal("explicit group D+0 did not override global D+1", quote, err)
	}
	original, err := s.ReserveTransfer(ctx, 681, "original-cycle", 100)
	if err != nil {
		t.Fatal(err)
	}
	setGroupSettlementCycle(t, s, 0, "1")
	if replay, err := s.ReserveTransfer(ctx, 681, "original-cycle", 100); err != nil || replay.ID != original.ID {
		t.Fatal("cycle update changed original transfer", replay, err)
	}
	if _, err := s.ReserveTransfer(ctx, 681, "new-cycle", 1); !errors.Is(err, ErrInsufficient) {
		t.Fatal("new transfer reused original maturity", err)
	}
}

func TestOriginalGroupSettlementCycleInheritanceMembershipExpiryAndCorruptPolicy(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	transferSetting(t, s, "settle_type", "1")
	for _, value := range []string{"", "_"} {
		setGroupSettlementCycle(t, s, 0, value)
		if funds, err := s.FundsAvailability(ctx, 681); err != nil || funds.SettleType != 1 || funds.Available != 0 {
			t.Fatal("group inheritance lost global D+1", value, funds, err)
		}
	}
	var vip int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"cycle VIP","enabled":true,"settle_type":"0"}') RETURNING id`).Scan(&vip); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(681,$1,clock_timestamp()+interval '1 day')`, vip); err != nil {
		t.Fatal(err)
	}
	if funds, err := s.FundsAvailability(ctx, 681); err != nil || funds.SettleType != 0 || funds.Available != 10000 {
		t.Fatal("active membership did not supply group D+0", funds, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE merchant_id=681`); err != nil {
		t.Fatal(err)
	}
	if funds, err := s.FundsAvailability(ctx, 681); err != nil || funds.SettleType != 1 || funds.Available != 0 {
		t.Fatal("expired membership still supplied group D+0", funds, err)
	}
	for _, value := range []string{"2", "-1", "01", "bad"} {
		setGroupSettlementCycle(t, s, 0, value)
		if _, err := s.FundsAvailability(ctx, 681); !errors.Is(err, ErrInvalid) {
			t.Fatal("corrupt stored cycle was treated as D+0", value, err)
		}
	}
}

func TestOriginalGroupSettlementCycleExpiryDuringConfirmationCannotReserve(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	setGroupSettlementCycle(t, s, 0, "1")
	var vip int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"expiring cycle VIP","enabled":true,"settle_type":"0"}') RETURNING id`).Scan(&vip); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(681,$1,clock_timestamp()+interval '1 second')`, vip); err != nil {
		t.Fatal(err)
	}
	guarded := WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
		if kind != "funds-request" || key != "internal-transfers:681:cycle-expiry" {
			return ErrInvalid
		}
		_, err := tx.Exec(c, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))::double precision+0.05) FROM memberships WHERE merchant_id=681`)
		return err
	})
	if _, err := s.RequestFunds(guarded, "internal-transfers", 681, 682, "cycle-expiry", 1, "merchant:681"); !errors.Is(err, ErrConflict) {
		t.Fatal("expired group cycle crossed confirmation and reserved funds", err)
	}
	if balance, err := s.Balance(ctx, 681); err != nil || balance.Available != 10000 || balance.Reserved != 0 {
		t.Fatal("expired group maturity moved money", balance, err)
	}
}

func TestOriginalGroupSettlementCyclePlanScopeKeepsLegacyAndBindsOverrides(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	plan := settlementPlanReady(t, s, 901)
	record, err := s.readSettlementPlan(ctx, s.pool, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := s.settlementPlanStaticScope(ctx, s.pool, record)
	if err != nil {
		t.Fatal(err)
	}
	merchantCycle := func(scope settlementPlanScope) (any, bool) {
		raw, _ := json.Marshal(scope.Merchants[0])
		var fields map[string]any
		if json.Unmarshal(raw, &fields) != nil {
			t.Fatal("invalid scope")
		}
		cycle, present := fields["settle_type"]
		return cycle, present
	}
	if _, present := merchantCycle(legacy); present {
		t.Fatal("inherited cycle changed historical scope shape")
	}
	authority, err := s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), plan.ID, plan.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"1", "0"} {
		setGroupSettlementCycle(t, s, 0, value)
		scope, err := s.settlementPlanStaticScope(ctx, s.pool, record)
		if err != nil {
			t.Fatal(err)
		}
		want := float64(1)
		if value == "0" {
			want = 0
		}
		if cycle, present := merchantCycle(scope); !present || cycle != want {
			t.Fatal("per-merchant cycle absent from authority scope", value, cycle, present)
		}
	}
	setGroupSettlementCycle(t, s, 0, "1")
	if err := verifySettlementPlanGlobalPolicy(ctx, s.pool, legacy); !errors.Is(err, ErrConflict) {
		t.Fatal("changed group maturity did not invalidate new generation", err)
	}
	if _, err := s.readSettlementPlanAuthorization(ctx, s.pool, authority.ID); err != nil {
		t.Fatal("cycle change reinterpreted historical signed authority", err)
	}
}

func TestOriginalGroupSettlementCycleMissingMerchantKeepsNotFound(t *testing.T) {
	s := testStore(t)
	if _, err := s.FundsAvailability(t.Context(), 999999); !errors.Is(err, ErrNotFound) {
		t.Fatal("cycle lookup changed missing wallet response", err)
	}
}

func TestOriginalGroupSettlementCycleThresholdPlanBindsMaturityAndKeepsCompletedReplay(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	run := thresholdPreparedRun74(t, s, 7450, func() {
		setGroupSettlementCycle(t, s, 0, "1")
		order, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7450, OutTradeNo: "group-cycle-threshold", Amount: 1000})
		if err != nil {
			t.Fatal(err)
		}
		if err = s.MarkPaid(ctx, order.TradeNo, "group-cycle-threshold-paid"); err != nil {
			t.Fatal(err)
		}
	})
	authority, err := s.readSettlementPlanAuthorization(ctx, s.pool, run.AuthorizationID)
	if err != nil || len(authority.Scope.Merchants) != 1 || authority.Scope.Merchants[0].SettleType == nil || *authority.Scope.Merchants[0].SettleType != 1 {
		t.Fatal("threshold authority omitted group cycle", authority, err)
	}
	if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	result, err := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if err != nil || len(result.Items) != 1 || result.CreatedCount != 0 || result.Items[0].Eligible != 0 || result.Items[0].Gross != 0 || result.Items[0].State != "skipped" {
		t.Fatal("threshold selection reserved today's group D+1 funds", result, err)
	}
	setGroupSettlementCycle(t, s, 0, "0")
	if err = s.GenerateSettlementPlanRun(ctx, run.ID); err != nil {
		t.Fatal("cycle change reinterpreted completed threshold selection", err)
	}
	var requests int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_requests").Scan(&requests); err != nil || requests != 0 {
		t.Fatal("completed threshold replay reserved newly eligible funds", requests, err)
	}
}
