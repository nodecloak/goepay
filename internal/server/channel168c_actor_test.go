package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestChannel168CDurableSuperAndAtomicAudit(t *testing.T) {
	for _, change := range []string{"demote", "revoke", "expiry", "audit-failure"} {
		t.Run(change, func(t *testing.T) {
			f := channel168Local(t)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			state := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))
			body := fmt.Sprintf(`{"daytop":"100","daymaxorder":"10","paymin":"0","paymax":"0","timestart":"","timestop":"","rate_limit_seconds":"0","rate_limit_count":"0","expected_fingerprint":%q}`, state["fingerprint"])
			if change == "expiry" || change == "audit-failure" {
				action := "PERFORM pg_sleep(1.3);"
				if change == "audit-failure" {
					action = "RAISE EXCEPTION 'synthetic channel168 audit failure';"
				}
				if _, e := f.p.Exec(ctx, `CREATE SEQUENCE channel168_audit_entered;CREATE FUNCTION channel168_audit_gate() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='runtime-channel-limits-update' THEN PERFORM nextval('channel168_audit_entered');`+action+`END IF;RETURN NEW;END$$;CREATE TRIGGER channel168_audit_gate BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION channel168_audit_gate()`); e != nil {
					t.Fatal(e)
				}
				if change == "expiry" {
					if _, e := f.p.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='admin168'`); e != nil {
						t.Fatal(e)
					}
				}
				w := f.admin("PATCH", "/7/limits", body, f.csrf)
				want := 503
				if change == "expiry" {
					want = 403
				}
				if w.Code != want {
					t.Fatal("audit/deadline status", w.Code, want)
				}
				var entered bool
				if e := f.p.QueryRow(ctx, "SELECT is_called FROM channel168_audit_entered").Scan(&entered); e != nil || !entered {
					t.Fatal("audit branch not reached", e)
				}
			} else {
				block, e := f.p.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer block.Rollback(context.Background())
				if _, e = block.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('goepay/route/channel/7',0))"); e != nil {
					t.Fatal(e)
				}
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- f.admin("PATCH", "/7/limits", body, f.csrf) }()
				for {
					var waiting bool
					if e = f.p.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))", block.Conn().PgConn().PID()).Scan(&waiting); e != nil {
						t.Fatal(e)
					}
					if waiting {
						break
					}
					select {
					case early := <-done:
						t.Fatal("no lock wait", early.Code)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(10 * time.Millisecond):
					}
				}
				sql := "DELETE FROM console_sessions WHERE username='admin168'"
				if change == "demote" {
					sql = "UPDATE console_users SET admin_class='operator' WHERE username='admin168'"
				}
				if _, e = f.p.Exec(ctx, sql); e != nil {
					t.Fatal(e)
				}
				if e = block.Rollback(ctx); e != nil {
					t.Fatal(e)
				}
				select {
				case w := <-done:
					if w.Code != 403 {
						t.Fatal("invalid durable actor wrote policy", w.Code)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			var policies, audits int
			if e := f.p.QueryRow(ctx, "SELECT (SELECT count(*) FROM runtime_channel_limits),(SELECT count(*) FROM backoffice_audit WHERE operation='runtime-channel-limits-update')").Scan(&policies, &audits); e != nil || policies != 0 || audits != 0 {
				t.Fatal("policy/audit failed to roll back", policies, audits, e)
			}
		})
	}
}
