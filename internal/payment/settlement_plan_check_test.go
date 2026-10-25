package payment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestSettlementPlanCheckerAuthenticatesSQLShapeWithoutDisablingGuards(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "local checker", APIKey: "local-only"}); err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var planID, definitionID, prefID int64
	if err = tx.QueryRow(ctx, "INSERT INTO settlement_plans DEFAULT VALUES RETURNING id").Scan(&planID); err != nil {
		t.Fatal(err)
	}
	schedule, _ := json.Marshal(SettlementScheduleSpec{Frequency: "hour", Interval: 1, AnchorAt: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)})
	if err = tx.QueryRow(ctx, `INSERT INTO settlement_plan_versions(plan_id,revision,name,mode,schedule,merchant_ids,actor_id,actor_name,proof_hash) VALUES($1,1,'local fake plan','generate_only',$2,ARRAY[901]::bigint[],1,'local-admin',decode(repeat('00',32),'hex')) RETURNING id`, planID, schedule).Scan(&definitionID); err != nil {
		t.Fatal(err)
	}
	if err = settlementPlanAudit(ctx, tx, "admin", 1, "local-admin", "settlement-plan-versions", definitionID, "define", map[string]string{"plan_id": "local"}); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `INSERT INTO settlement_schedule_preferences(merchant_id,revision,enabled,actor_name,proof_hash) VALUES(901,1,false,'local-owner',decode(repeat('00',32),'hex')) RETURNING id`).Scan(&prefID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO settlement_schedule_preference_heads(merchant_id,preference_id,revision) VALUES(901,$1,1)", prefID); err != nil {
		t.Fatal(err)
	}
	if err = settlementPlanAudit(ctx, tx, "merchant", 901, "local-owner", "settlement-schedule-preferences", prefID, "configure", map[string]string{"state": "disabled"}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal("complete SQL shape should commit without disabling any guard", err)
	}
	if _, err = s.SettlementPlan(ctx, planID, settlementPlanAdmin); !errors.Is(err, ErrConflict) {
		t.Fatal("public read adopted unsigned SQL definition", err)
	}
	report, err := s.CheckLedger(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(report)
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	if count, ok := fields["invalid_settlement_plans"].(float64); !ok || count < 2 {
		t.Fatal("checker ignored unsigned definition/preference", string(raw))
	}
}
