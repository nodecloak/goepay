package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sync/atomic"
	"testing"
)

type risk177QueryCounter struct{ n atomic.Int64 }

func (c *risk177QueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}
func (*risk177QueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestRisk177ActionChainAuditReadIsBatchedAndRetainsMissingProofDetection(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	key, e := s.coreRiskIPKey("192.0.2.177")
	if e != nil {
		t.Fatal(e)
	}
	appendPair := func() {
		t.Helper()
		tx, e := s.pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		now, e := coreRiskDBNow(ctx, tx)
		if e != nil {
			t.Fatal(e)
		}
		a := coreRiskAction{Kind: "ip", SubjectKey: key, Action: "block", Rule: "auto_check_payip", At: now, Sources: []int64{}}
		if _, e = s.writeCoreRiskAction(ctx, tx, a); e != nil {
			t.Fatal(e)
		}
		a.Action = "release"
		a.ActorRole = "admin"
		a.ActorID = 177
		a.ActorName = "synthetic-risk-admin"
		if _, e = s.writeCoreRiskAction(ctx, tx, a); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
	}
	appendPair()
	counter := &risk177QueryCounter{}
	cfg := s.pool.Config()
	cfg.ConnConfig.Tracer = counter
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	reader, e := NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	read := func() (int64, error) {
		tx, e := pool.Begin(ctx)
		if e != nil {
			return 0, e
		}
		defer tx.Rollback(ctx)
		counter.n.Store(0)
		_, _, e = reader.readCoreRiskAction(ctx, tx, "ip", key)
		return counter.n.Load(), e
	}
	two, e := read()
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 9; i++ {
		appendPair()
	}
	twenty, e := read()
	if e != nil || two != twenty || twenty != 2 {
		t.Fatal("action audit SQL grew per revision", two, twenty, e)
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE backoffice_audit DISABLE TRIGGER USER; DELETE FROM backoffice_audit WHERE id=(SELECT min(id)FROM backoffice_audit WHERE resource='core-risk-actions'); ALTER TABLE backoffice_audit ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = read(); !errors.Is(e, ErrConflict) {
		t.Fatal("batched reader ignored missing independent action audit", e)
	}
	t.Logf("authenticated action chain read: two revisions=%d, twenty revisions=%d SQL round trips", two, twenty)
}
