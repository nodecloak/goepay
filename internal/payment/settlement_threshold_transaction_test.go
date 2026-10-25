package payment

import (
	"context"
	"testing"
)

func TestSettlementThreshold74SharedItemPersistenceLeavesCommitToItsOwningRun(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 7404)
	a, e := s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, p.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	item := settlementPlanItemRecord{RunID: run.ID, View: SettlementPlanItemView{MerchantID: 7404, State: "skipped", Reason: "insufficient_funds"}}
	if e = s.persistSettlementPlanItemTx(ctx, tx, item, run, a); e != nil {
		t.Fatal(e)
	}
	var external int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM settlement_plan_items WHERE run_id=$1", run.ID).Scan(&external); e != nil || external != 0 {
		t.Fatal("item primitive committed part of a future atomic selection/run", external, e)
	}
	if e = tx.Rollback(ctx); e != nil {
		t.Fatal("item primitive closed owning transaction", e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM settlement_plan_items WHERE run_id=$1", run.ID).Scan(&external); e != nil || external != 0 {
		t.Fatal("owning run could not roll back item/audit", external, e)
	}
}
