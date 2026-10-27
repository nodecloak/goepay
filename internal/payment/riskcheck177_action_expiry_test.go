package payment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRisk177HistoricalActionExpiryUsesDatabaseClockAndAuditDetectsOlderHead(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	now, e := coreRiskDBNow(ctx, tx)
	if e != nil {
		t.Fatal(e)
	}
	until := now.Add(time.Second)
	key, e := s.coreRiskIPKey("192.0.2.177")
	if e != nil {
		t.Fatal(e)
	}
	a := coreRiskAction{Kind: "ip", SubjectKey: key, Action: "block", Rule: "auto_check_payip", PolicyRevision: 1, At: until.Add(-5 * 24 * time.Hour), Until: &until, Sources: []int64{177}}
	if _, e = s.writeCoreRiskAction(ctx, tx, a); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	tx, e = s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	current, e := coreRiskDBNow(ctx, tx)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.checkCoreRiskActions(ctx, tx, []string{"192.0.2.177"}, "", 1, 7, current); !errors.Is(e, ErrState) {
		t.Fatal("seeded original five-day ban not active before deadline", e)
	}
	if _, e = tx.Exec(ctx, `SELECT pg_sleep(1.1)`); e != nil {
		t.Fatal(e)
	}
	current, e = coreRiskDBNow(ctx, tx)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.checkCoreRiskActions(ctx, tx, []string{"192.0.2.177"}, "", 1, 7, current); e != nil {
		t.Fatal("expired ban denied new admission", e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	tx, e = s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	a.At = current
	a.Until = nil
	if _, e = s.writeCoreRiskAction(ctx, tx, a); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE core_risk_actions DISABLE TRIGGER USER;DELETE FROM core_risk_actions WHERE revision=2;ALTER TABLE core_risk_actions ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	tx, e = s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, _, e = s.readCoreRiskAction(ctx, tx, "ip", key); !errors.Is(e, ErrConflict) {
		t.Fatal("older full valid action head was trusted despite retained newer audit", e)
	}
}
