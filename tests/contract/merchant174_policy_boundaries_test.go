package contract_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMerchant174PersonalPolicyRequiresStrictFlagsAndOriginalCSRF(t *testing.T) {
	f := signatureFixture(t, 2)
	state := apiSwitchState174(t, f.request("GET", "api-switches", "", f.csrf))
	body := apiSwitchBody174(state["fingerprint"].(string), 1, 1, 0)
	if w := f.request("PATCH", "api-switches", body, "bad"); w.Code != 403 {
		t.Fatal("CSRF not checked", w.Code)
	}
	for _, path := range []string{"api-switches?merchant_id=1", "api-switches?", "api-switches/2"} {
		w := f.request("GET", path, "", f.csrf)
		if w.Code != 400 && w.Code != 404 {
			t.Fatal("caller target/query override accepted", path, w.Code)
		}
	}
	for _, raw := range []string{`{}`, `[]`, `null`, body + `{}`, strings.TrimSuffix(body, "}") + `,"refund":0}`, strings.TrimSuffix(body, "}") + `,"merchant_id":"1"}`, strings.Replace(body, `"keylogin":1`, `"keylogin":true`, 1), strings.Replace(body, `"keylogin":1`, `"keylogin":"1"`, 1), strings.Replace(body, `"keylogin":1`, `"keylogin":1.0`, 1), strings.Replace(body, `"keylogin":1`, `"keylogin":1e0`, 1), strings.Replace(body, `"keylogin":1`, `"keylogin":2`, 1), strings.Replace(body, `"keylogin":1`, `"keylogin":null`, 1)} {
		w := f.request("PATCH", "api-switches", raw, f.csrf)
		if w.Code != 400 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("ambiguous policy flags accepted/cacheable", w.Code)
		}
	}
	var policies, audits int
	if e := f.p.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM merchant_api_switches),(SELECT count(*) FROM backoffice_audit WHERE operation='merchant-api-switches-update')`).Scan(&policies, &audits); e != nil || policies+audits != 0 {
		t.Fatal("bad input wrote policy/audit", policies, audits, e)
	}
}
func TestMerchant174PersonalPolicySameCASHasOneWinner(t *testing.T) {
	f := signatureFixture(t, 2)
	state := apiSwitchState174(t, f.request("GET", "api-switches", "", f.csrf))
	body := apiSwitchBody174(state["fingerprint"].(string), 1, 0, 1)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- f.request("PATCH", "api-switches", body, f.csrf).Code }()
	}
	wg.Wait()
	close(codes)
	win, conflict := 0, 0
	for code := range codes {
		switch code {
		case 200:
			win++
		case 409:
			conflict++
		default:
			t.Fatal("unexpected CAS status", code)
		}
	}
	if win != 1 || conflict != 1 {
		t.Fatal("multiple personal CAS winners", win, conflict)
	}
	state = apiSwitchState174(t, f.request("GET", "api-switches", "", f.csrf))
	apiSwitchState174(t, f.request("PATCH", "api-switches", apiSwitchBody174(state["fingerprint"].(string), 0, 1, 0), f.csrf))
}
func TestMerchant174PersonalPolicyAuditFailureAndLastWriteExpiryRollback(t *testing.T) {
	for _, mode := range []string{"expiry", "audit-failure"} {
		t.Run(mode, func(t *testing.T) {
			f := signatureFixture(t, 2)
			ctx := context.Background()
			state := apiSwitchState174(t, f.request("GET", "api-switches", "", f.csrf))
			var before int64
			if e := f.p.QueryRow(ctx, `SELECT config_revision FROM merchants WHERE id=2`).Scan(&before); e != nil {
				t.Fatal(e)
			}
			action := "PERFORM pg_sleep(1.3);"
			if mode == "audit-failure" {
				action = "RAISE EXCEPTION 'synthetic policy audit failure';"
			}
			if _, e := f.p.Exec(ctx, `CREATE SEQUENCE merchant174_audit_entered;CREATE FUNCTION merchant174_audit_gate() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='merchant-api-switches-update' THEN PERFORM nextval('merchant174_audit_entered');`+action+`END IF;RETURN NEW;END$$;CREATE TRIGGER merchant174_audit_gate BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION merchant174_audit_gate()`); e != nil {
				t.Fatal(e)
			}
			if mode == "expiry" {
				if _, e := f.p.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='merchant'`); e != nil {
					t.Fatal(e)
				}
			}
			w := f.request("PATCH", "api-switches", apiSwitchBody174(state["fingerprint"].(string), 0, 0, 0), f.csrf)
			want := 503
			if mode == "expiry" {
				want = 403
			}
			if w.Code != want {
				t.Fatal("failed authority/audit committed personal policy", mode, w.Code, want)
			}
			var policies, audits int
			var revision int64
			var entered bool
			if e := f.p.QueryRow(ctx, `SELECT (SELECT config_revision FROM merchants WHERE id=2),(SELECT count(*) FROM merchant_api_switches),(SELECT count(*) FROM backoffice_audit WHERE operation='merchant-api-switches-update'),(SELECT is_called FROM merchant174_audit_entered)`).Scan(&revision, &policies, &audits, &entered); e != nil || revision != before || policies+audits != 0 || !entered {
				t.Fatal("policy/revision/audit failed rollback or branch not reached", revision, policies, audits, entered, e)
			}
		})
	}
}

func merchant174WaitForBlocker(t *testing.T, f signatureFixture163, pid uint32, ctx context.Context) {
	t.Helper()
	for {
		var waiting bool
		if e := f.p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))`, pid).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func TestMerchant174PersonalPolicyRevocationAtMerchantAndSessionWaitRejectsWrite(t *testing.T) {
	for _, stage := range []string{"merchant", "session"} {
		t.Run(stage, func(t *testing.T) {
			f := signatureFixture(t, 2)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			state := apiSwitchState174(t, f.request("GET", "api-switches", "", f.csrf))
			block, e := f.p.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer block.Rollback(context.Background())
			if stage == "merchant" {
				_, e = block.Exec(ctx, `SELECT id FROM merchants WHERE id=2 FOR UPDATE`)
			} else {
				_, e = block.Exec(ctx, `DELETE FROM console_sessions WHERE username='merchant'`)
			}
			if e != nil {
				t.Fatal(e)
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- f.request("PATCH", "api-switches", apiSwitchBody174(state["fingerprint"].(string), 0, 0, 0), f.csrf)
			}()
			merchant174WaitForBlocker(t, f, block.Conn().PgConn().PID(), ctx)
			if stage == "merchant" {
				if _, e = f.p.Exec(ctx, `DELETE FROM console_sessions WHERE username='merchant'`); e != nil {
					t.Fatal(e)
				}
				e = block.Rollback(ctx)
			} else {
				e = block.Commit(ctx)
			}
			if e != nil {
				t.Fatal(e)
			}
			select {
			case w := <-done:
				if w.Code != 403 {
					t.Fatal("revoked durable session wrote policy", stage, w.Code)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var count int
			if e = f.p.QueryRow(ctx, `SELECT count(*) FROM merchant_api_switches`).Scan(&count); e != nil || count != 0 {
				t.Fatal(fmt.Sprint("revocation left policy at ", stage), count, e)
			}
		})
	}
}
