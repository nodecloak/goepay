package contract_test

import (
	"context"
	"goepay/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPasswordChangeCannotIssueOldPasswordSessionInFlight(t *testing.T) {
	s, pool, _ := fixture(t)
	s.Sessions = identity.NewPersistentSessions(pool, false)
	ctx := context.Background()
	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(ctx)
	if _, err = lock.Exec(ctx, "LOCK TABLE console_sessions IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/console/v1/admin/login", strings.NewReader(`{"username":"admin","password":"contract-password-123"}`))
		s.Handler().ServeHTTP(w, r)
		done <- w
	}()
	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='console_sessions'::regclass AND NOT granted)").Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("login never reached session persistence barrier")
	}
	if _, err = pool.Exec(ctx, "UPDATE console_users SET password_hash='replaced-by-concurrent-password-change' WHERE username='admin' AND role='admin'"); err != nil {
		t.Fatal(err)
	}
	if err = lock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case w := <-done:
		if w.Code == 200 || len(w.Result().Cookies()) != 0 {
			t.Fatalf("old password issued valid session: %d", w.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("login did not finish")
	}
}
