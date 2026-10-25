package payment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSettlementThreshold74NormalDailyParentCannotBeRescheduledAfterIntentChildLoss(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mid := int64(7435)
	p := settlementPlanReady(t, s, mid)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "normal-daily-original-income", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "normal-daily-original-local"); e != nil {
		t.Fatal(e)
	}
	guarded := settlementPlanGuard(t, s)
	a, e := s.AuthorizeSettlementPlan(guarded, p.ID, p.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, r.ID); e != nil {
		t.Fatal(e)
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
	auth := settlementPlanAuthorizationInput(t, s)
	auth.ExpectedAuthorizationID = a.ID
	auth.MaxDayGross = 2000
	auth.MaxDayItems = 2
	if _, e = s.AuthorizeSettlementPlan(guarded, p.ID, p.Revision, auth, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	next, e := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	bad, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer bad.Rollback(ctx)
	if _, e = bad.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
		t.Fatal(e)
	}
	if _, e = bad.Exec(ctx, "DELETE FROM settlement_scheduled_intents WHERE merchant_id=$1", mid); e != nil {
		t.Fatal(e)
	}
	if e = bad.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, next.ID); !errors.Is(e, ErrConflict) {
		t.Fatal("missing ordinary intent child allowed a second normal daily reserve", e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_requests").Scan(&count); e != nil || count != 1 {
		t.Fatal("damaged original source created new normal financial intent", count, e)
	}
}
