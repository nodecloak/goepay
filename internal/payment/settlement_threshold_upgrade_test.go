package payment

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestSettlementThreshold74UpgradePreservesActual73NormalRequestProofAndDailyIntent(t *testing.T) {
	s := membership72HistoricalStore(t, 73)
	ctx := context.Background()
	mid := int64(7433)
	p := settlementPlanReady(t, s, mid)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "threshold-upgrade-original-income", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "threshold-upgrade-original-local"); e != nil {
		t.Fatal(e)
	}
	guard := settlementPlanGuard(t, s)
	a, e := s.AuthorizeSettlementPlan(guard, p.ID, p.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	// Build a positive schema73 normal request using its original shared money
	// primitive and old SQL constraints. No74 source table or column exists yet;
	// today's readRun intentionally cannot be used before the additive migration.
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var itemID, configID int64
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('settlement_plan_items','id'))").Scan(&itemID); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, "SELECT recipient_config_id FROM settlement_schedule_preference_heads h JOIN settlement_schedule_preferences p ON p.id=h.preference_id WHERE h.merchant_id=$1", mid).Scan(&configID); e != nil {
		t.Fatal(e)
	}
	external := fmt.Sprintf("SP%d", itemID)
	requestCtx := WithFundsActionGuard(ctx, func(_ context.Context, got pgx.Tx, kind, key string) error {
		if got != tx || kind != "funds-request" || key != fmt.Sprintf("settlements:%d:%s", mid, external) {
			return ErrInvalid
		}
		return nil
	})
	request, e := s.requestFundsTxSettlement(requestCtx, tx, "settlements", mid, 0, external, 1000, "system:settlement-scheduler", &SettlementExpectation{NetAmount: 1000}, &configID)
	if e != nil {
		t.Fatal(e)
	}
	item := settlementPlanItemRecord{RunID: run.ID, View: SettlementPlanItemView{ID: itemID, MerchantID: mid, RequestID: request.ID, State: "created", Reason: "pending_review", LedgerAvailable: 2000, Eligible: 2000, Gross: 1000, Net: 1000, RecipientConfigID: configID}}
	if e = s.persistSettlementPlanItemRowsTx(ctx, tx, item, run, a, false); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal("real schema73 original constraints rejected fixture", e)
	}
	var before, after []byte
	if e = s.pool.QueryRow(ctx, "SELECT proof_hash FROM settlement_plan_authorizations WHERE id=$1", a.ID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	tables := []string{"settlement_plan_versions", "settlement_plan_authorizations", "settlement_plan_runs", "settlement_plan_items", "settlement_plan_budget_uses", "settlement_scheduled_intents", "ledger_requests", "settlement_request_terms", "settlement_fundings", "accounts", "journals", "ledger_entries", "backoffice_audit"}
	hash := func(table string) string {
		t.Helper()
		var out string
		if e = s.pool.QueryRow(ctx, "SELECT md5(COALESCE(jsonb_agg(to_jsonb(t)-ARRAY['threshold_source_version','threshold_source_hash','threshold_source_sequence'] ORDER BY (to_jsonb(t)-ARRAY['threshold_source_version','threshold_source_hash','threshold_source_sequence'])::text),'[]'::jsonb)::text) FROM "+table+" t").Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	prior := map[string]string{}
	for _, table := range tables {
		prior[table] = hash(table)
	}
	raw, e := os.ReadFile("../../db/migrations/000074_settlement_threshold.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	for _, table := range tables {
		if hash(table) != prior[table] {
			t.Fatal("additive migration rewrote original73 rows", table)
		}
	}
	if e = s.pool.QueryRow(ctx, "SELECT proof_hash FROM settlement_plan_authorizations WHERE id=$1", a.ID).Scan(&after); e != nil || !bytes.Equal(before, after) {
		t.Fatal("original authority proof changed", e)
	}
	var version, sequence, children int
	var source []byte
	if e = s.pool.QueryRow(ctx, "SELECT threshold_source_version,threshold_source_sequence,threshold_source_hash,(SELECT count(*) FROM settlement_threshold_selections) FROM settlement_plan_runs WHERE id=$1", run.ID).Scan(&version, &sequence, &source, &children); e != nil || version != 0 || sequence != 0 || source != nil || children != 0 {
		t.Fatal("historical normal run received invented threshold proof", version, sequence, children, e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
		t.Fatal("original normal replay no longer valid", e)
	}
	v, e := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if e != nil || v.CreatedCount != 1 || len(v.Items) != 1 || v.Items[0].RequestID != request.ID {
		t.Fatal(v, e)
	}
	current, e := s.readSettlementPlan(ctx, s.pool, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	in := current.Input
	in.Schedule.AnchorAt = in.Schedule.AnchorAt.Add(-time.Hour)
	p, e = s.UpdateSettlementPlan(ctx, p.ID, p.Revision, in, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	input := settlementPlanAuthorizationInput(t, s)
	input.ExpectedAuthorizationID = a.ID
	input.MaxDayGross = 2000
	input.MaxDayItems = 2
	if _, e = s.AuthorizeSettlementPlan(guard, p.ID, p.Revision, input, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	next, e := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, next.ID); e != nil {
		t.Fatal(e)
	}
	v, e = s.SettlementPlanRun(ctx, next.ID, settlementPlanAdmin)
	if e != nil || v.CreatedCount != 0 || len(v.Items) != 1 || v.Items[0].Reason != "already_scheduled" {
		t.Fatal("old normal daily uniqueness expanded by threshold mode", v, e)
	}
	// All73->74 compatibility assertions above precede the later schema. The
	// latest whole-system checker requires75/76 only after those assertions.
	for _, name := range []string{"000075_preauth_remaining_release.up.sql", "000076_channel_auto_stop.up.sql"} {
		later, e := os.ReadFile("../../db/migrations/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.pool.Exec(ctx, string(later)); e != nil {
			t.Fatal(e)
		}
	}
	if report, e := s.CheckLedger(ctx); e != nil || report != (LedgerCheck{}) {
		t.Fatal(report, e)
	}
}
