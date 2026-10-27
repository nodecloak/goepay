package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/identity"
)

func scheduler171Body(fp string) string {
	return fmt.Sprintf(`{"enabled":true,"schedule":{"frequency":"once","interval":1,"execute_at":%q},"params":{"limit":20},"expected_fingerprint":%q}`, time.Now().UTC().Add(time.Hour).Truncate(time.Second).Format(time.RFC3339), fp)
}
func TestScheduler171LegacyMetadataCannotBeEnabledThroughGenericCRUD(t *testing.T) {
	f := scheduler171Local(t)
	r := httptest.NewRequest("POST", "/console/v1/admin/resources/cron-tasks", strings.NewReader(`{"name":"unused-plan","kind":"payment.notify","interval_seconds":60,"enabled":true}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("legacy metadata still accepted an active-looking configuration", w.Code)
	}
}
func TestScheduler171DurablePermissionAndLateAuditAreAtomic(t *testing.T) {
	for _, mode := range []string{"revoke", "demote", "expiry", "audit-failure"} {
		t.Run(mode, func(t *testing.T) {
			f := scheduler171Local(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			state := scheduler171Data(t, f.request("GET", "/referral_daily", "", f.csrf))
			body := scheduler171Body(state["fingerprint"].(string))
			if mode == "expiry" || mode == "audit-failure" {
				action := "PERFORM pg_sleep(1.3);"
				if mode == "audit-failure" {
					action = "RAISE EXCEPTION 'synthetic scheduler171 audit fault';"
				}
				if _, e := f.s.Pool.Exec(ctx, `CREATE SEQUENCE scheduler171_audit_entered;CREATE FUNCTION scheduler171_audit_gate()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='scheduled-tasks' THEN PERFORM nextval('scheduler171_audit_entered');`+action+`END IF;RETURN NEW;END$$;CREATE TRIGGER scheduler171_audit_gate BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION scheduler171_audit_gate()`); e != nil {
					t.Fatal(e)
				}
				if mode == "expiry" {
					if _, e := f.s.Pool.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='scheduler171'`); e != nil {
						t.Fatal(e)
					}
				}
				w := f.request("PATCH", "/referral_daily", body, f.csrf)
				want := 403
				if mode == "audit-failure" {
					want = 503
				}
				if w.Code != want {
					t.Fatal("late authority/audit failed but saved task", mode, w.Code)
				}
				var entered bool
				if e := f.s.Pool.QueryRow(ctx, `SELECT is_called FROM scheduler171_audit_entered`).Scan(&entered); e != nil || !entered {
					t.Fatal("last-write gate not reached", e)
				}
			} else {
				block, e := f.s.Pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer block.Rollback(context.Background())
				if _, e = block.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('goepay/scheduled-task/referral_daily',0))`); e != nil {
					t.Fatal(e)
				}
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- f.request("PATCH", "/referral_daily", body, f.csrf) }()
				for {
					var waiting bool
					if e = f.s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()AND $1::integer=ANY(pg_blocking_pids(pid)))`, block.Conn().PgConn().PID()).Scan(&waiting); e != nil {
						t.Fatal(e)
					}
					if waiting {
						break
					}
					select {
					case early := <-done:
						t.Fatal("task did not wait", early.Code)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(10 * time.Millisecond):
					}
				}
				sql := `DELETE FROM console_sessions WHERE username='scheduler171'`
				if mode == "demote" {
					sql = `UPDATE console_users SET admin_class='operator' WHERE username='scheduler171'`
				}
				if _, e = f.s.Pool.Exec(ctx, sql); e != nil {
					t.Fatal(e)
				}
				if e = block.Rollback(ctx); e != nil {
					t.Fatal(e)
				}
				select {
				case w := <-done:
					if w.Code != 403 {
						t.Fatal("invalid durable actor committed", mode, w.Code)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			var policies, runs, audits int
			if e := f.s.Pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM scheduled_tasks),(SELECT count(*)FROM scheduled_task_runs),(SELECT count(*)FROM backoffice_audit WHERE resource='scheduled-tasks')`).Scan(&policies, &runs, &audits); e != nil || policies != 0 || runs != 0 || audits != 0 {
				t.Fatal("rejected write changed task/intent/audit", policies, runs, audits, e)
			}
		})
	}
}
func TestScheduler171OperatorMerchantAndDelegationCannotControlRegistry(t *testing.T) {
	f := scheduler171Local(t)
	ctx := context.Background()
	if _, e := f.s.Pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('scheduler171-operator','admin',172,'Synthetic Operator','unused-hash','operator');INSERT INTO console_users(username,role,subject_id,display_name,password_hash)VALUES('scheduler171-merchant','merchant',1,'Synthetic Merchant','unused-hash')`); e != nil {
		t.Fatal(e)
	}
	operator := identity.User{ID: 172, Username: "scheduler171-operator", Role: "admin", AdminClass: "operator", AuthVersion: 1}
	operatorToken, operatorCSRF, e := f.s.Sessions.Create(operator)
	if e != nil {
		t.Fatal(e)
	}
	other := f
	other.token, other.csrf = operatorToken, operatorCSRF
	if w := other.request("GET", "", "", other.csrf); w.Code != 403 {
		t.Fatal("operator read privileged registry", w.Code)
	}
	merchant := identity.User{ID: 1, Username: "scheduler171-merchant", Role: "merchant", AuthVersion: 1}
	merchantToken, _, e := f.s.Sessions.Create(merchant)
	if e != nil {
		t.Fatal(e)
	}
	check := func(token string) {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/merchant/scheduled-tasks", nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("merchant/delegated registry control", w.Code)
		}
	}
	check(merchantToken)
	tx, e := f.s.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	issuer := identity.User{ID: 171, Username: "scheduler171", Role: "admin", AdminClass: "super", AuthVersion: 1}
	delegated, _, _, e := f.s.Sessions.CreateDelegated(ctx, tx, merchant, issuer, f.token)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	check(delegated)
}
