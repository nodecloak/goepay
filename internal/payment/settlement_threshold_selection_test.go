package payment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func thresholdRun74(t *testing.T, s *Store, mid int64) SettlementPlanRunView {
	return thresholdPreparedRun74(t, s, mid, nil)
}

func thresholdPreparedRun74(t *testing.T, s *Store, mid int64, prepare func()) SettlementPlanRunView {
	t.Helper()
	ctx := context.Background()
	p := settlementPlanReady(t, s, mid)
	if prepare != nil {
		prepare()
	}
	in := settlementPlanInputFor(mid)
	in.Mode = settlementThresholdMode
	in.Schedule = SettlementScheduleSpec{Frequency: "minute", Interval: 1, AnchorAt: time.Now().UTC().Truncate(time.Minute)}
	var preparedDBClock time.Time
	if e := s.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&preparedDBClock); e != nil {
		t.Fatal(e)
	}
	p, e := s.UpdateSettlementPlan(ctx, p.ID, p.Revision, in, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "auto_settle_money", "1.00")
	transferSetting(t, s, "transfer_alipay", "7")
	authority := settlementPlanAuthorizationInput(t, s)
	if _, e = s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, p.Revision, authority, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	run, e := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin)
	if e != nil {
		var failedDBClock time.Time
		clockErr := s.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&failedDBClock)
		slot, scheduledAt, slotErr := dueSettlementSlot(in.Schedule, failedDBClock)
		t.Fatalf("threshold run preparation: %v; host_utc=%s prepared_db_utc=%s failed_db_utc=%s clock_error=%v anchor=%s slot=%d scheduled_at=%s slot_error=%v authorization_from=%s authorization_until=%s budgets=%d/%d/%d/%d", e, time.Now().UTC().Format(time.RFC3339Nano), preparedDBClock.UTC().Format(time.RFC3339Nano), failedDBClock.UTC().Format(time.RFC3339Nano), clockErr, in.Schedule.AnchorAt.Format(time.RFC3339Nano), slot, scheduledAt.UTC().Format(time.RFC3339Nano), slotErr, authority.ValidFrom.UTC().Format(time.RFC3339Nano), authority.ExpiresAt.UTC().Format(time.RFC3339Nano), authority.MaxItemGross, authority.MaxRunGross, authority.MaxDayGross, authority.MaxDayItems)
	}
	return run
}

func TestSettlementThreshold74SelectionAnchorCannotBeErasedOrReselectAfterChildLoss(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run := thresholdPreparedRun74(t, s, 7405, func() {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7405, OutTradeNo: "threshold74-closed-income", Amount: 1000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.MarkPaid(ctx, o.TradeNo, "threshold74-closed-local"); e != nil {
			t.Fatal(e)
		}
		var gid int64
		if e = s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('groups','{"name":"closed threshold local","settle_open":2}')RETURNING id`).Scan(&gid); e != nil {
			t.Fatal(e)
		}
		if _, e = s.pool.Exec(ctx, "UPDATE merchants SET base_group_id=$1 WHERE id=7405", gid); e != nil {
			t.Fatal(e)
		}
	})
	r, e := s.readSettlementPlanRun(ctx, s.pool, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.readSettlementPlanAuthorization(ctx, s.pool, run.AuthorizationID)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	selected, e := s.sealThresholdSelection(ctx, tx, r)
	if e != nil {
		t.Fatal(e)
	}
	if len(selected.Snapshot.Candidates) != 1 || len(selected.Snapshot.Selected) != 1 || selected.Snapshot.Selected[0] != "7405" {
		t.Fatal("closed group must remain in original top-five selection", selected)
	}
	if e = s.persistSettlementPlanItemTx(ctx, tx, settlementPlanItemRecord{RunID: run.ID, View: selected.Snapshot.Planned[0]}, run, a.View); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.readThresholdSelection(ctx, s.pool, r); e != nil {
		t.Fatal("committed original selection unreadable", e)
	}
	if report, e := s.CheckLedger(ctx); e != nil || report != (LedgerCheck{}) {
		t.Fatal("valid original selection rejected by full checker", report, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE settlement_plan_runs SET threshold_source_version=0,threshold_source_hash=NULL,threshold_source_sequence=0 WHERE id=$1", run.ID); e == nil {
		t.Fatal("original run anchor was erasable")
	}
	bad, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer bad.Rollback(ctx)
	if _, e = bad.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
		t.Fatal(e)
	}
	if _, e = bad.Exec(ctx, "DELETE FROM settlement_threshold_selections WHERE run_id=$1", run.ID); e != nil {
		t.Fatal(e)
	}
	if e = bad.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.readThresholdSelection(ctx, s.pool, r); !errors.Is(e, ErrConflict) {
		t.Fatal("missing selection became new pending work", e)
	}
	if report, e := s.CheckLedger(ctx); e != nil || report.InvalidSettlementPlans == 0 {
		t.Fatal("full checker omitted threshold parent with missing choice", report, e)
	}
	newTx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer newTx.Rollback(ctx)
	if _, e = s.sealThresholdSelection(ctx, newTx, r); !errors.Is(e, ErrConflict) {
		t.Fatal("lost original choice was regenerated from current wallet", e)
	}
}

func TestSettlementThreshold74SelectionCannotCommitWithoutEveryOriginallySelectedItem(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run := thresholdPreparedRun74(t, s, 7406, func() {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7406, OutTradeNo: "threshold74-positive-income", Amount: 1000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.MarkPaid(ctx, o.TradeNo, "threshold74-positive-local"); e != nil {
			t.Fatal(e)
		}
	})
	r, e := s.readSettlementPlanRun(ctx, s.pool, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = s.sealThresholdSelection(ctx, tx, r); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e == nil {
		t.Fatal("partial original choice committed without its run outcomes")
	}
	var version, children int
	if e = s.pool.QueryRow(ctx, "SELECT threshold_source_version,(SELECT count(*) FROM settlement_threshold_selections WHERE run_id=$1) FROM settlement_plan_runs WHERE id=$1", run.ID).Scan(&version, &children); e != nil || version != 0 || children != 0 {
		t.Fatal("failed whole-run commit left an irreversible partial anchor", version, children, e)
	}
}

func TestSettlementThreshold74EmptyOriginalSelectionIsCompletedAndCannotBecomeFreshWork(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run := thresholdRun74(t, s, 7407)
	r, e := s.readSettlementPlanRun(ctx, s.pool, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	selected, e := s.sealThresholdSelection(ctx, tx, r)
	if e != nil || len(selected.Snapshot.Selected) != 0 {
		t.Fatal(selected, e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal("an authenticated empty selection needs no fake skipped items", e)
	}
	view, e := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if e != nil || view.State != "completed" || len(view.Items) != 0 {
		t.Fatal("empty original choice stayed pending and could be retried with new wallet funds", view, e)
	}
}
