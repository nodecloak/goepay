package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
	"goepay/internal/server"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginThrottleSurvivesNewServerAndSourcePort(t *testing.T) {
	s, pool, _ := fixture(t)
	attempt := func(app *server.Server, username, remote string) int {
		r := httptest.NewRequest("POST", "/console/v1/admin/login", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"invalid-password"}`, username)))
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		app.Handler().ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < 10; i++ {
		if status := attempt(s, "unknown-user", fmt.Sprintf("192.0.2.9:%d", 1000+i)); status != 401 {
			t.Fatalf("early throttle %d: %d", i, status)
		}
	}
	other := &server.Server{Pool: pool, Store: s.Store, Sessions: identity.NewPersistentSessions(pool, false)}
	if status := attempt(other, "unknown-user", "192.0.2.9:9000"); status != 429 {
		t.Fatalf("new server reset persisted attempts: %d", status)
	}
	if status := attempt(other, "different-user", "192.0.2.9:9000"); status != 401 {
		t.Fatalf("one account blocked all accounts: %d", status)
	}
	if _, e := pool.Exec(context.Background(), "UPDATE console_login_throttle SET window_start=statement_timestamp()-interval '16 minutes'"); e != nil {
		t.Fatal(e)
	}
	if status := attempt(other, "unknown-user", "192.0.2.9:9000"); status != 401 {
		t.Fatalf("expired throttle retained: %d", status)
	}
	if _, e := pool.Exec(context.Background(), "DROP TABLE console_login_throttle"); e != nil {
		t.Fatal(e)
	}
	if status := attempt(other, "known-or-new", "192.0.2.9:9000"); status != 503 {
		t.Fatalf("throttle dependency failure was bypassed: %d", status)
	}
}

func TestCleanupPreviewsAndRemovesOnlyExpiredLoginThrottle(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	// Two old hashed keys and one active key, never a credential or identity.
	if _, e := pool.Exec(ctx, `INSERT INTO console_login_throttle(key_hash,window_start,attempts,updated_at) VALUES(decode(repeat('01',32),'hex'),statement_timestamp()-interval '3 days',1,statement_timestamp()-interval '3 days'),(decode(repeat('02',32),'hex'),statement_timestamp()-interval '3 days',1,statement_timestamp()-interval '3 days'),(decode(repeat('03',32),'hex'),statement_timestamp(),1,statement_timestamp())`); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "admin", ID: 1, Role: "admin"})
	if e != nil {
		t.Fatal(e)
	}
	for _, preview := range []bool{true, false} {
		body := fmt.Sprintf(`{"preview":%t,"cutoff":%q}`, preview, time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339))
		r := httptest.NewRequest("POST", "/console/v1/admin/operations/cleanup", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var result struct {
			Data struct {
				Expired int64 `json:"expired_login_throttle_keys"`
			}
		}
		if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil {
			t.Fatal(e)
		}
		if w.Code != 200 || result.Data.Expired != 2 {
			t.Fatalf("cleanup preview=%v status=%d expired=%d", preview, w.Code, result.Data.Expired)
		}
		var n int
		if e = pool.QueryRow(ctx, "SELECT count(*) FROM console_login_throttle").Scan(&n); e != nil {
			t.Fatal(e)
		}
		want := 1
		if preview {
			want = 3
		}
		if n != want {
			t.Fatalf("preview=%v keys=%d want=%d", preview, n, want)
		}
	}
}

func TestLoginThrottleAggregatesRotatingUsernames(t *testing.T) {
	s, _, _ := fixture(t)
	for i := 0; i < 301; i++ {
		r := httptest.NewRequest("POST", "/console/v1/merchant/login", strings.NewReader(fmt.Sprintf(`{"username":"unknown-%d","password":"invalid-password"}`, i)))
		r.RemoteAddr = fmt.Sprintf("[2001:db8::1]:%d", 1000+i)
		// Equivalent IPv6 spellings and changing source ports are one source.
		if i%2 == 0 {
			r.RemoteAddr = fmt.Sprintf("[2001:0db8:0:0:0:0:0:1]:%d", 1000+i)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		want := 401
		if i == 300 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d status=%d want=%d", i+1, w.Code, want)
		}
	}
}

func TestLoginThrottleIsAtomicAcrossConcurrentServerInstances(t *testing.T) {
	s, pool, _ := fixture(t)
	// Measure contention of already connected application instances. Cold
	// Windows-to-Docker TCP setup is not the persisted counter's concurrency
	// contract and must not consume its fixed two-second production budget.
	connections := min(30, int(pool.Config().MaxConns))
	setupContext, cancelSetup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelSetup()
	warmed := make([]*pgxpool.Conn, 0, connections)
	defer func() {
		for _, connection := range warmed {
			connection.Release()
		}
	}()
	for i := 0; i < connections; i++ {
		connection, err := pool.Acquire(setupContext)
		if err != nil {
			t.Fatal(err)
		}
		warmed = append(warmed, connection)
		if _, err = connection.Exec(setupContext, "SELECT 1"); err != nil {
			t.Fatal(err)
		}
	}
	for _, connection := range warmed {
		connection.Release()
	}
	warmed = nil
	var wg sync.WaitGroup
	var ready sync.WaitGroup
	start := make(chan struct{})
	ready.Add(30)
	var accepted atomic.Int64
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ready.Done()
			<-start
			app := &server.Server{Pool: pool, Store: s.Store, Sessions: identity.NewPersistentSessions(pool, false)}
			r := httptest.NewRequest("POST", "/console/v1/admin/login", strings.NewReader(`{"username":"absent-concurrent","password":"invalid-password"}`))
			r.RemoteAddr = fmt.Sprintf("192.0.2.101:%d", 1000+i)
			w := httptest.NewRecorder()
			app.Handler().ServeHTTP(w, r)
			if w.Code == 401 {
				accepted.Add(1)
			} else if w.Code != 429 {
				t.Errorf("concurrent login returned %d", w.Code)
			}
		}(i)
	}
	ready.Wait()
	close(start)
	wg.Wait()
	if n := accepted.Load(); n > 10 || n == 0 {
		t.Fatalf("accepted credential checks=%d want1..10", n)
	}
	var count int
	if e := pool.QueryRow(context.Background(), "SELECT count(*) FROM console_login_throttle").Scan(&count); e != nil || count != 2 {
		t.Fatalf("hashed counter keys=%d err=%v", count, e)
	}
}
