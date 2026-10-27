package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
)

type batch185NotifyTraceKey struct{}
type batch185NotifyTrace struct {
	entered, release chan struct{}
	used             atomic.Bool
}

func (g *batch185NotifyTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(d.SQL, "SELECT state FROM jobs WHERE kind='payment.notify'") && g.used.CompareAndSwap(false, true) {
		return context.WithValue(ctx, batch185NotifyTraceKey{}, true)
	}
	return ctx
}
func (g *batch185NotifyTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	if ctx.Value(batch185NotifyTraceKey{}) == true && d.Err == nil {
		close(g.entered)
		select {
		case <-g.release:
		case <-ctx.Done():
		}
	}
}

func TestOrderBatch185WorkerCompletionCannotRequeueAnAlreadyDeliveredOriginalJob(t *testing.T) {
	f, _, _ := subchannel168BProductLocal(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if out := subchannel168BProductPay(t, f, "notify185-worker-race", "native", map[string]string{"notify_url": "https://synthetic.example.invalid/notify"}); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(ctx, 1, "notify185-worker-race")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	q := jobs.Queue{Pool: f.p}
	job, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	body := strings.Replace(batch185Input(o.TradeNo, "notify185-worker-race"), `"action":"query"`, `"action":"notify"`, 1)
	w := batch185Request(f, "POST", "", body)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var v struct{ Data struct{ ID string } }
	if json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal(w.Body.String())
	}
	trace := &batch185NotifyTrace{entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(trace.release) }) }
	defer release()
	cfg := f.p.Config()
	cfg.ConnConfig.Tracer = trace
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { release(); pool.Close() }()
	store, e := payment.NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	f.s.Store = store
	executed := make(chan *httptest.ResponseRecorder, 1)
	go func() { executed <- batch185Request(f, "POST", "/"+v.Data.ID+"/execute", `{"limit":1}`) }()
	select {
	case <-trace.entered:
	case <-ctx.Done():
		t.Fatal("execution did not reach actual notification decision", ctx.Err())
	}
	completed := make(chan error, 1)
	go func() { completed <- q.Complete(ctx, job) }()
	workerFinished := false
	select {
	case e = <-completed:
		if e != nil {
			t.Fatal(e)
		}
		workerFinished = true
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case w = <-executed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !workerFinished {
		select {
		case e = <-completed:
			if e != nil {
				t.Fatal(e)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var count int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind='payment.notify' AND payload->>'trade_no'=$1`, o.TradeNo).Scan(&count); e != nil || count != 1 {
		t.Fatal("original worker completion caused a duplicate delivery intent", count, e, w.Body.String())
	}
}
