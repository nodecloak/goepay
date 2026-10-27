package server

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/payment"
	"sync/atomic"
	"testing"
)

type subchannel168BQueryCounter struct{ n atomic.Int64 }

func (c *subchannel168BQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}
func (c *subchannel168BQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}
func TestSubchannel168BActualPGCandidateReadsStayBatchedWithManySameParentChildren(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	for i := 0; i < 2; i++ {
		child168BCreate(t, f, fmt.Sprintf("batch-child-%d", i), true)
	}
	counter := &subchannel168BQueryCounter{}
	cfg := f.p.Config()
	cfg.ConnConfig.Tracer = counter
	pool, e := pgxpool.NewWithConfig(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	store, e := payment.NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	f.s.Pool = pool
	f.s.Store = store
	f.s.ProviderFlow.Store = store
	counter.n.Store(0)
	if _, _, e = f.s.providerRoutedRequest(context.Background(), 1, child168BParams("batch-two-children", "1.00")); e != nil {
		t.Fatal(e)
	}
	two := counter.n.Load()
	for i := 2; i < 20; i++ {
		child168BCreate(t, f, fmt.Sprintf("batch-child-%d", i), true)
	}
	counter.n.Store(0)
	if _, _, e = f.s.providerRoutedRequest(context.Background(), 1, child168BParams("batch-twenty-children", "1.00")); e != nil {
		t.Fatal(e)
	}
	twenty := counter.n.Load()
	if two != twenty {
		t.Fatal("child data/locks/readiness acquired per-row SQL round trips", two, twenty)
	}
	t.Logf("same-parent child route query count: two=%d twenty=%d; no upstream I/O", two, twenty)
}
