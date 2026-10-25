package payment

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestSettlementThreshold74GeneratesOriginalFiveAtomicWholeWalletsAndSecondRunUsesDayBudget(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	var p SettlementPlanView
	for mid := int64(7421); mid <= 7426; mid++ {
		v := settlementPlanReady(t, s, mid)
		if mid == 7421 {
			p = v
		}
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("threshold74-income-%d", mid), Amount: 1000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.MarkPaid(ctx, o.TradeNo, fmt.Sprintf("threshold-local-%d", mid)); e != nil {
			t.Fatal(e)
		}
		if mid == 7426 || mid == 7424 || mid == 7423 {
			body := `{"name":"threshold group","settle_open":2}`
			if mid == 7424 {
				body = `{"name":"zero rate","settle_rate":0}`
			}
			if mid == 7423 {
				body = `{"name":"lower rate","settle_rate":"1.25"}`
			}
			var gid int64
			if e = s.pool.QueryRow(ctx, "INSERT INTO backoffice_records(resource,body)VALUES('groups',$1)RETURNING id", body).Scan(&gid); e != nil {
				t.Fatal(e)
			}
			if _, e = s.pool.Exec(ctx, "UPDATE merchants SET base_group_id=$1 WHERE id=$2", gid, mid); e != nil {
				t.Fatal(e)
			}
		}
	}
	for key, value := range map[string]string{"auto_settle_money": "1.00", "transfer_alipay": "7", "settlement_fee_rate_bps": "300", "settlement_fee_maximum_cents": "25", "settlement_fee_minimum_cents": "900", "settlement_retained_cents": "800", "settlement_minimum_cents": "999999", "settlement_maximum_cents": "1", "settlement_daily_limit": "1"} {
		transferSetting(t, s, key, value)
	}
	in := settlementPlanInputFor(7421)
	in.Mode = settlementThresholdMode
	in.MerchantIDs = []int64{7421, 7422, 7423, 7424, 7425, 7426}
	in.Schedule = SettlementScheduleSpec{Frequency: "minute", Interval: 1, AnchorAt: time.Now().UTC().Truncate(time.Minute)}
	p, e := s.UpdateSettlementPlan(ctx, p.ID, p.Revision, in, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	a := settlementPlanAuthorizationInput(t, s)
	a.MaxRunGross = 3000
	a.MaxDayGross = 5000
	a.MaxDayItems = 5
	if _, e = s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, p.Revision, a, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	r, e := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, r.ID); e != nil {
		t.Fatal("whole-balance threshold generation", e)
	}
	v, e := s.SettlementPlanRun(ctx, r.ID, settlementPlanAdmin)
	if e != nil || v.State != "completed" || len(v.Items) != 5 || v.CreatedCount != 3 || v.SkippedCount != 2 {
		t.Fatal(v, e)
	}
	byMid := map[int64]SettlementPlanItemView{}
	for _, i := range v.Items {
		byMid[i.MerchantID] = i
	}
	if byMid[7426].Reason != "group_closed" || byMid[7422].Reason != "budget_exhausted" || byMid[7421].ID != 0 {
		t.Fatal(byMid)
	}
	for mid, want := range map[int64]int64{7425: 25, 7424: 0, 7423: 13} {
		i := byMid[mid]
		if i.State != "created" || i.Gross != 1000 || i.Fee != want || i.Net != 1000-want || i.Retained != 0 || i.RequestID <= 0 {
			t.Fatal(i)
		}
		var available, reserved int64
		if e = s.pool.QueryRow(ctx, "SELECT COALESCE(sum(CASE WHEN kind='available' THEN balance ELSE 0 END),0)::bigint,COALESCE(sum(CASE WHEN kind='settlement_reserve' THEN balance ELSE 0 END),0)::bigint FROM accounts WHERE owner_id=$1", mid).Scan(&available, &reserved); e != nil || available != 0 || reserved != 1000 {
			t.Fatal("original whole-wallet reserve", available, reserved, e)
		}
	}
	var oldIntents, thresholdIntents int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM settlement_scheduled_intents),(SELECT count(*) FROM settlement_threshold_intents)").Scan(&oldIntents, &thresholdIntents); e != nil || oldIntents != 0 || thresholdIntents != 3 {
		t.Fatal(oldIntents, thresholdIntents, e)
	}
	// A new definition and authority retain the original day consumption. The
	// helper's smaller1000 day cap must not create another request or reset it.
	r2 := thresholdNextRun74(t, s, r)
	if e = s.GenerateSettlementPlanRun(ctx, r2.ID); e != nil {
		t.Fatal(e)
	}
	v2, e := s.SettlementPlanRun(ctx, r2.ID, settlementPlanAdmin)
	if e != nil || v2.CreatedCount != 0 || len(v2.Items) != 3 {
		t.Fatal(v2, e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, r.ID); e != nil {
		t.Fatal("original completed replay after replacement", e)
	}
	var requests int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_requests").Scan(&requests); e != nil || requests != 3 {
		t.Fatal("replay or replacement created money", requests, e)
	}
	if report, e := s.CheckLedger(ctx); e != nil || report != (LedgerCheck{}) {
		t.Fatal(report, e)
	}
}

func TestSettlementThreshold74SecondRunPaysNewWholeBalanceAndMissingOlderBudgetCannotRefill(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	r := thresholdPreparedRun74(t, s, 7427, func() {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7427, OutTradeNo: "threshold-first-income", Amount: 1000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.MarkPaid(ctx, o.TradeNo, "threshold-first-local"); e != nil {
			t.Fatal(e)
		}
	})
	if e := s.GenerateSettlementPlanRun(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7427, OutTradeNo: "threshold-second-income", Amount: 700})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "threshold-second-local"); e != nil {
		t.Fatal(e)
	}
	limits := func(a *SettlementPlanAuthorizationInput) { a.MaxDayGross = 3000; a.MaxDayItems = 3 }
	r2 := thresholdNextRunLimits74(t, s, r, limits)
	if e = s.GenerateSettlementPlanRun(ctx, r2.ID); e != nil {
		t.Fatal("same-day threshold continuation", e)
	}
	v, e := s.SettlementPlanRun(ctx, r2.ID, settlementPlanAdmin)
	if e != nil || v.CreatedCount != 1 || len(v.Items) != 1 || v.Items[0].Gross != 700 {
		t.Fatal("new whole balance was blocked by ordinary daily uniqueness", v, e)
	}
	source, e := s.readSettlementPlanRun(ctx, s.pool, r2.ID)
	if e != nil || source.selection == nil || source.selection.Snapshot.BeforeGross != 1000 || source.selection.Snapshot.AfterGross != 1700 || source.selection.Snapshot.AfterItems != 2 {
		t.Fatal(source, e)
	}
	if report, e := s.CheckLedger(ctx); e != nil || report != (LedgerCheck{}) {
		t.Fatal(report, e)
	}
	r3 := thresholdNextRunLimits74(t, s, r2, limits)
	bad, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer bad.Rollback(ctx)
	if _, e = bad.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
		t.Fatal(e)
	}
	if _, e = bad.Exec(ctx, "DELETE FROM settlement_plan_budget_uses WHERE item_id IN(SELECT id FROM settlement_plan_items WHERE run_id=$1)", r.ID); e != nil {
		t.Fatal(e)
	}
	if e = bad.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, r3.ID); e == nil {
		t.Fatal("missing older budget child refilled signed day total")
	}
}
