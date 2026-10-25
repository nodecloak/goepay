package contract_test

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
	"goepay/internal/server"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Delay consumption of counter results to reproduce client/network scheduling
// latency. Explicit transactions keep their row locks during this wait;
// an atomic auto-committed statement releases them before the client delay.
type loginCounterLatency struct{}
type loginCounterLatencyKey struct{}

func (loginCounterLatency) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, loginCounterLatencyKey{}, strings.Contains(d.SQL, "console_login_throttle"))
}
func (loginCounterLatency) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if counter, _ := ctx.Value(loginCounterLatencyKey{}).(bool); counter {
		time.Sleep(80 * time.Millisecond)
	}
}

func TestLoginThrottleClientLatencyDoesNotSerializeReplicaRoundTrips(t *testing.T) {
	s, original, _ := fixture(t)
	ctx := context.Background()
	cfg := original.Config()
	cfg.ConnConfig.Tracer = loginCounterLatency{}
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	warm := make([]*pgxpool.Conn, 0, min(30, int(pool.Config().MaxConns)))
	defer func() {
		for _, c := range warm {
			c.Release()
		}
	}()
	for i := 0; i < cap(warm); i++ {
		c, e := pool.Acquire(ctx)
		if e != nil {
			t.Fatal(e)
		}
		warm = append(warm, c)
		if _, e = c.Exec(ctx, "SELECT 1"); e != nil {
			t.Fatal(e)
		}
	}
	for _, c := range warm {
		c.Release()
	}
	warm = nil
	var ready, wg sync.WaitGroup
	ready.Add(30)
	start := make(chan struct{})
	var checks, busy, unavailable atomic.Int64
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ready.Done()
			<-start
			app := &server.Server{Pool: pool, Store: s.Store, Sessions: identity.NewPersistentSessions(pool, false)}
			r := httptest.NewRequest("POST", "/console/v1/admin/login", strings.NewReader(`{"username":"absent-delayed","password":"invalid-password"}`))
			r.RemoteAddr = fmt.Sprintf("192.0.2.108:%d", 1000+i)
			w := httptest.NewRecorder()
			app.Handler().ServeHTTP(w, r)
			switch w.Code {
			case 401:
				checks.Add(1)
			case 429:
				busy.Add(1)
			case 503:
				unavailable.Add(1)
			default:
				t.Errorf("unexpected login status %d", w.Code)
			}
		}(i)
	}
	ready.Wait()
	close(start)
	wg.Wait()
	if unavailable.Load() != 0 {
		t.Fatalf("healthy delayed DB exhausted the fixed counter deadline: checks=%d limited=%d unavailable=%d", checks.Load(), busy.Load(), unavailable.Load())
	}
	if checks.Load() < 1 || checks.Load() > 10 || checks.Load()+busy.Load() != 30 {
		t.Fatalf("persistent attempt budget lost: checks=%d limited=%d", checks.Load(), busy.Load())
	}
	var counters, source, account int
	if e = pool.QueryRow(ctx, "SELECT count(*),max(attempts),min(attempts) FROM console_login_throttle").Scan(&counters, &source, &account); e != nil || counters != 2 || source != 30 || account != 11 {
		t.Fatalf("counter accounting %d %d %d %v", counters, source, account, e)
	}
}
