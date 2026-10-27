package contract_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMerchant174PendingMFAKeyLoginRechecksVersionCloseAndRotation(t *testing.T) {
	for _, change := range []string{"version", "keylogin-close", "key-rotation"} {
		t.Run(change, func(t *testing.T) {
			f := signatureFixture(t, 2)
			secret, f := merchant174EnrollMFA(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			m, e := f.s.Store.Merchant(ctx, 2)
			if e != nil {
				t.Fatal(e)
			}
			block, e := f.p.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer block.Rollback(context.Background())
			if _, e = block.Exec(ctx, `SELECT username FROM console_mfa WHERE username='merchant' AND role='merchant' FOR UPDATE`); e != nil {
				t.Fatal(e)
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- merchant174KeyLogin(f, "2", m.APIKey, merchant174TOTP(t, secret, time.Now())) }()
			merchant174WaitForBlocker(t, f, block.Conn().PgConn().PID(), ctx)
			switch change {
			case "version":
				if _, e = f.p.Exec(ctx, `UPDATE console_users SET auth_version=auth_version+1 WHERE username='merchant' AND role='merchant';DELETE FROM console_sessions WHERE username='merchant'`); e != nil {
					t.Fatal(e)
				}
			case "keylogin-close":
				merchant174Set(t, f, 0, 1, 0)
			case "key-rotation":
				m.APIKey = "synthetic-rotated-key174"
				var profile []byte
				if e = f.p.QueryRow(ctx, `SELECT profile FROM merchants WHERE id=2`).Scan(&profile); e != nil {
					t.Fatal(e)
				}
				if e = f.s.Store.ReplaceMerchantAtomic(ctx, m, profile, nil); e != nil {
					t.Fatal(e)
				}
			}
			if e = block.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case w := <-done:
				if w.Code != 401 || len(w.Result().Cookies()) != 0 {
					t.Fatal("stale prepared key login issued session", change, w.Code)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var logins int
			if e = f.p.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE operation='merchant-key-login'`).Scan(&logins); e != nil || logins != 0 {
				t.Fatal("rejected key login committed success audit", logins, e)
			}
		})
	}
}
func TestMerchant174KeyLoginMandatoryAuditFailureRollsBackSession(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	m, e := f.s.Store.Merchant(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	var before int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM console_sessions`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(ctx, `CREATE FUNCTION merchant174_fail_key_audit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='merchant-key-login' THEN RAISE EXCEPTION 'synthetic key audit failure';END IF;RETURN NEW;END$$;CREATE TRIGGER merchant174_fail_key_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION merchant174_fail_key_audit()`); e != nil {
		t.Fatal(e)
	}
	w := merchant174KeyLogin(f, "2", m.APIKey, "")
	if w.Code != 503 || len(w.Result().Cookies()) != 0 {
		t.Fatal("key login ignored mandatory audit failure", w.Code)
	}
	var after int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM console_sessions`).Scan(&after); e != nil || after != before {
		t.Fatal("failed key login left durable session", before, after, e)
	}
}
