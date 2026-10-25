package payment

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func thresholdNextRun74(t *testing.T, s *Store, previous SettlementPlanRunView) SettlementPlanRunView {
	return thresholdNextRunLimits74(t, s, previous, nil)
}

func thresholdNextRunLimits74(t *testing.T, s *Store, previous SettlementPlanRunView, limits func(*SettlementPlanAuthorizationInput)) SettlementPlanRunView {
	t.Helper()
	ctx := context.Background()
	p, e := s.readSettlementPlan(ctx, s.pool, previous.PlanID)
	if e != nil {
		t.Fatal(e)
	}
	in := p.Input
	in.Name = fmt.Sprintf("threshold next revision %d", p.View.Revision+1)
	in.Schedule.AnchorAt = in.Schedule.AnchorAt.Add(-time.Minute)
	v, e := s.UpdateSettlementPlan(ctx, p.View.ID, p.View.Revision, in, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	a := settlementPlanAuthorizationInput(t, s)
	a.ExpectedAuthorizationID = v.LastAuthorizationID
	if limits != nil {
		limits(&a)
	}
	guarded := WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
		if kind != "settlement-plan-authorize" {
			return ErrInvalid
		}
		_, e := tx.Exec(c, "INSERT INTO local_plan_grants(action_key)VALUES($1)", key)
		return e
	})
	if _, e = s.AuthorizeSettlementPlan(guarded, v.ID, v.Revision, a, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	r, e := s.CreateSettlementPlanRun(ctx, v.ID, v.Revision, settlementPlanAdmin)
	if e != nil || r.ID == previous.ID {
		t.Fatal(r, e)
	}
	return r
}

func sealEmptyThreshold74(t *testing.T, s *Store, run SettlementPlanRunView) settlementThresholdSelection {
	t.Helper()
	ctx := context.Background()
	r, e := s.readSettlementPlanRun(ctx, s.pool, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	v, e := s.sealThresholdSelection(ctx, tx, r)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	return v
}

func TestSettlementThreshold74CumulativeSourceChainSurvivesReauthorizationAndRejectsOlderChildLoss(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	r1 := thresholdRun74(t, s, 7410)
	v1 := sealEmptyThreshold74(t, s, r1)
	r2 := thresholdNextRun74(t, s, r1)
	v2 := sealEmptyThreshold74(t, s, r2)
	if v1.Sequence <= 0 || v2.Sequence <= v1.Sequence || v2.Snapshot.PreviousRunID != r1.ID || v2.Snapshot.PreviousSequence != v1.Sequence || !bytes.Equal(v2.Snapshot.PreviousProof, v1.Proof) || v2.Snapshot.BeforeGross != 0 || v2.Snapshot.AfterGross != 0 || v2.Snapshot.Planned == nil {
		t.Fatal("missing original cumulative chain", v1, v2)
	}
	r3 := thresholdNextRun74(t, s, r2)
	bad, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer bad.Rollback(ctx)
	if _, e = bad.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
		t.Fatal(e)
	}
	if _, e = bad.Exec(ctx, "DELETE FROM settlement_threshold_selections WHERE run_id=$1", r1.ID); e != nil {
		t.Fatal(e)
	}
	if e = bad.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	r, e := s.readSettlementPlanRun(ctx, s.pool, r3.ID)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = s.sealThresholdSelection(ctx, tx, r); e == nil {
		t.Fatal("loss of nonlatest original child reset cumulative source")
	}
}
