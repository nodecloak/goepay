package contract_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/compat"
	"goepay/internal/identity"
)

func runtimeAdmin179(t *testing.T, f signatureFixture163) adminCredentials162 {
	t.Helper()
	var version int64
	if e := f.p.QueryRow(context.Background(), `SELECT auth_version FROM console_users WHERE username='admin' AND role='admin'`).Scan(&version); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := f.s.Sessions.Create(identity.User{ID: 1, Role: "admin", Username: "admin", AdminClass: "super", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	return adminCredentials162{s: f.s, pool: f.p, token: token, csrf: csrf}
}

func TestGlobalAccess179DurableOriginRejectsMissingFlagsHeadAndLegalOldRows(t *testing.T) {
	for _, damage := range []string{"flag-loss", "head-loss", "old-valid-rows"} {
		t.Run(damage, func(t *testing.T) {
			f, p, o := merchant174Runtime(t)
			c := runtimeAdmin179(t, f)
			ctx := context.Background()
			state := platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
			state = platformState179(t, platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "0", "1"), c.csrf))
			var oldRows string
			if e := f.p.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_object('id',id,'body',body))::text FROM backoffice_records WHERE resource='settings' AND body->>'key' IN('close_keylogin','user_refund','__platform_merchant_access179')`).Scan(&oldRows); e != nil {
				t.Fatal(e)
			}
			platformState179(t, platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "1", "0"), c.csrf))
			var e error
			switch damage {
			case "flag-loss":
				_, e = f.p.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'='close_keylogin'`)
			case "head-loss":
				_, e = f.p.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'='__platform_merchant_access179'`)
			case "old-valid-rows":
				_, e = f.p.Exec(ctx, `UPDATE backoffice_records r SET body=x.body FROM jsonb_to_recordset($1::jsonb) AS x(id bigint,body jsonb) WHERE r.id=x.id`, oldRows)
			}
			if e != nil {
				t.Fatal(e)
			}
			if w := platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf); w.Code != 409 {
				t.Fatal("configured origin became a new default", w.Code)
			}
			w := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/console/v1/public/merchant-access", nil))
			if w.Code != 503 {
				t.Fatal("public capability fabricated a known setting", w.Code)
			}
			if w = merchant174KeyLogin(f, "2", "contract-rsa-secret", ""); w.Code != 503 || len(w.Result().Cookies()) != 0 {
				t.Fatal("damaged origin issued a key session", w.Code)
			}
			out := merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "damaged-platform179", "money": "1.00"})
			if out["code"] != float64(-1) || p.refundWrites.Load() != 0 {
				t.Fatal("damaged origin admitted new money movement", out, p.refundWrites.Load())
			}
		})
	}
}

func TestGlobalAccess179AuditFailureAndPostWriteExpiryRollBackAllSettings(t *testing.T) {
	for _, mode := range []string{"audit-failure", "expiry-after-audit"} {
		t.Run(mode, func(t *testing.T) {
			c := adminCredentialsFixture162(t)
			ctx := context.Background()
			state := platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
			body := platformPatch179(state["fingerprint"].(string), "1", "0")
			code := "RAISE EXCEPTION 'synthetic audit failure';"
			want := 503
			if mode == "expiry-after-audit" {
				code = "PERFORM nextval('access179_audit_entered');PERFORM pg_sleep(1.3);"
				want = 403
			}
			if _, e := c.pool.Exec(ctx, `CREATE SEQUENCE access179_audit_entered;CREATE FUNCTION access179_audit_gate() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='platform-merchant-access179' THEN `+code+` END IF;RETURN NEW;END$$;CREATE TRIGGER access179_audit_gate BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION access179_audit_gate()`); e != nil {
				t.Fatal(e)
			}
			if mode == "expiry-after-audit" {
				if _, e := c.pool.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='admin'`); e != nil {
					t.Fatal(e)
				}
			}
			if w := platformAdmin179(c, "PATCH", "settings/merchant-access", body, c.csrf); w.Code != want {
				t.Fatal("late actor failure did not roll back", w.Code, want)
			}
			var records, audits int
			if e := c.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM backoffice_records WHERE resource='settings' AND body->>'key' IN('close_keylogin','user_refund','__platform_merchant_access179')),(SELECT count(*) FROM backoffice_audit WHERE resource='platform-merchant-access179')`).Scan(&records, &audits); e != nil || records != 0 || audits != 0 {
				t.Fatal("partial platform policy survived failed audit/expiry", records, audits, e)
			}
			if mode == "expiry-after-audit" {
				var entered bool
				if e := c.pool.QueryRow(ctx, `SELECT is_called FROM access179_audit_entered`).Scan(&entered); e != nil || !entered {
					t.Fatal("expiry fixture never reached the last waiting write", e)
				}
			}
		})
	}
}

func TestGlobalAccess179SameValueUnrelatedFieldsAndPrivateHeadReadBoundary(t *testing.T) {
	c := adminCredentialsFixture162(t)
	ctx := context.Background()
	state := platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
	state = platformState179(t, platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "0", "1"), c.csrf))
	if _, e := c.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"qq":"123456789","unrelated179":"preserved"}'::jsonb,config_revision=config_revision+1 WHERE id=2;INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"site_name","value":"Synthetic 179"}')`); e != nil {
		t.Fatal(e)
	}
	again := platformState179(t, platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "0", "1"), c.csrf))
	if again["fingerprint"] == state["fingerprint"] {
		t.Fatal("same-value committed update did not advance independent revision")
	}
	var head int64
	if e := c.pool.QueryRow(ctx, `SELECT id FROM backoffice_records WHERE resource='settings' AND body->>'key'='__platform_merchant_access179'`).Scan(&head); e != nil {
		t.Fatal(e)
	}
	if w := platformAdmin179(c, "GET", fmt.Sprintf("resources/settings/%d", head), "", c.csrf); w.Code != 404 {
		t.Fatal("private authenticity source entered generic detail", w.Code)
	}
	w := platformAdmin179(c, "GET", "resources/settings?page=1&limit=100", "", c.csrf)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "__platform_merchant_access179") || strings.Contains(w.Body.String(), "close_id") || strings.Contains(w.Body.String(), "proof") {
		t.Fatal("private authenticity source entered generic list", w.Code)
	}
	for _, path := range []string{"settings/merchant-access?x=1", "settings/merchant-access?"} {
		if w := platformAdmin179(c, "GET", path, "", c.csrf); w.Code != 400 {
			t.Fatal("platform settings accepted query context", path, w.Code)
		}
	}
}

func TestGlobalAccess179OriginalSessionRevokeWhileGlobalSaveWaits(t *testing.T) {
	c := adminCredentialsFixture162(t)
	state := platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, e := c.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	tx, e := conn.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `DELETE FROM console_sessions WHERE username='admin'`); e != nil {
		t.Fatal(e)
	}
	result := make(chan int, 1)
	go func() {
		result <- platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "1", "0"), c.csrf).Code
	}()
	for {
		var waiting bool
		if e = c.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)))`, int(conn.Conn().PgConn().PID())).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case status := <-result:
			t.Fatal("request did not wait on controlled durable revocation", status)
		case <-ctx.Done():
			t.Fatal("request never reached controlled session lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if status := <-result; status != 403 {
		t.Fatal("revoked original session wrote policy", status)
	}
	var rows int
	if e = c.pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_records WHERE resource='settings' AND body->>'key' IN('close_keylogin','user_refund','__platform_merchant_access179')`).Scan(&rows); e != nil || rows != 0 {
		t.Fatal("revoked actor left a partial policy", rows, e)
	}
}
