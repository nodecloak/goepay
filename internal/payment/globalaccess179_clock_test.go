package payment

import (
	"context"
	"goepay/internal/identity"
	"testing"
	"time"
)

func TestGlobalAccess179PreparedGrantFinalClockAfterAuditWait(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 17901, Name: "synthetic", APIKey: "local-key179"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('key179','merchant',17901,'Local','synthetic')`); e != nil {
		t.Fatal(e)
	}
	grant, e := s.PrepareMerchantKeyLogin(ctx, 17901, "local-key179")
	if e != nil {
		t.Fatal(e)
	}
	sessions := identity.NewPersistentSessions(s.pool, false)
	if _, e = s.pool.Exec(ctx, `CREATE SEQUENCE access179_wait;CREATE FUNCTION access179_delay() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='merchant-key-login' THEN PERFORM nextval('access179_wait');PERFORM pg_sleep(2.5);END IF;RETURN NEW;END$$;CREATE TRIGGER access179_delay BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION access179_delay()`); e != nil {
		t.Fatal(e)
	}
	var dbNow time.Time
	if e = s.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&dbNow); e != nil {
		t.Fatal(e)
	}
	grant.until = dbNow.Add(2 * time.Second)
	if _, token, _, e := s.CompleteMerchantKeyLogin(ctx, grant, sessions); e == nil || token != "" {
		t.Fatal("grant expired during last write committed a session", e)
	}
	var called bool
	var count int
	if e = s.pool.QueryRow(ctx, `SELECT is_called FROM access179_wait`).Scan(&called); e != nil || !called {
		t.Fatal("did not pass the controlled final-write wait", called, e)
	}
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM console_sessions)+(SELECT count(*) FROM backoffice_audit WHERE operation='merchant-key-login')`).Scan(&count); e != nil || count != 0 {
		t.Fatal("expired grant left session/audit", count, e)
	}
}
