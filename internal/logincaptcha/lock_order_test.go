package logincaptcha

import (
	"context"
	"testing"
	"time"
)

// The normal settings writer locks an existing record before its UPDATE or
// DELETE obtains ROW EXCLUSIVE on the table. The login reader must not hold
// table SHARE while waiting for that writer's tuple lock.
func TestLoginCaptcha205SessionDoesNotDeadlockWithExistingSettingUpdateOrDelete(t *testing.T) {
	for _, operation := range []string{"update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			s, pool, sessions, user := sessionFixture205(t, 0)
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			var recordID int64
			if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"captcha_open_login","value":"0"}')RETURNING id`).Scan(&recordID); e != nil {
				t.Fatal(e)
			}
			grant := disabledGrant205(t, s)
			writer, e := pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
				defer stop()
				_ = writer.Rollback(cleanup)
			}()
			if _, e = writer.Exec(ctx, `SET LOCAL lock_timeout='4s'`); e != nil {
				t.Fatal(e)
			}
			var body string
			if e = writer.QueryRow(ctx, `SELECT body::text FROM backoffice_records WHERE resource='settings' AND id=$1 FOR UPDATE`, recordID).Scan(&body); e != nil {
				t.Fatal("settings writer could not take its original tuple lock", e)
			}
			type result struct {
				token, csrf string
				err         error
			}
			done := make(chan result, 1)
			go func() {
				token, csrf, err := s.IssueSession(ctx, grant, user, sessions)
				done <- result{token: token, csrf: csrf, err: err}
			}()
			var login result
			finished := false
			// Correct MVCC readers may commit before this observation. Do not
			// make seeing their short-lived lock a condition of this regression.
			for !finished {
				select {
				case login = <-done:
					finished = true
				default:
				}
				if finished {
					break
				}
				var held bool
				if e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='relation' AND relation='backoffice_records'::regclass AND mode='ShareLock' AND granted)`).Scan(&held); e != nil {
					t.Fatal("could not observe login's original policy lock", e)
				}
				if held {
					break
				}
				select {
				case login = <-done:
					finished = true
				case <-ctx.Done():
					t.Fatal("login neither finished nor acquired its policy lock", ctx.Err())
				case <-time.After(5 * time.Millisecond):
				}
			}
			if operation == "update" {
				_, e = writer.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"1"'::jsonb) WHERE resource='settings' AND id=$1`, recordID)
			} else {
				_, e = writer.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND id=$1`, recordID)
			}
			if e != nil {
				t.Errorf("original settings %s must complete without deadlock or lock timeout: %v", operation, e)
				// Release its tuple before waiting for the login result, including
				// when PostgreSQL chose this writer as the deadlock victim.
				_ = writer.Rollback(ctx)
			} else if e = writer.Commit(ctx); e != nil {
				t.Errorf("original settings %s failed to commit: %v", operation, e)
			}
			if !finished {
				select {
				case login = <-done:
					finished = true
				case <-ctx.Done():
					t.Fatal("session did not finish after settings writer released", ctx.Err())
				}
			}
			if login.err != nil || len(login.token) != 43 || len(login.csrf) != 43 {
				t.Errorf("original login must commit without deadlock or lock timeout: error=%v token_length=%d csrf_length=%d", login.err, len(login.token), len(login.csrf))
			}
			var count int
			if e = pool.QueryRow(ctx, `SELECT count(*) FROM console_sessions WHERE username='login205' AND role='merchant'`).Scan(&count); e != nil || count != 1 {
				t.Errorf("one original session must survive the serialized settings change: sessions=%d query_error=%v", count, e)
			}
		})
	}
}
