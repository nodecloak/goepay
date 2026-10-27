package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/identity"
)

// These tests exercise the existing HTTP contract (or the existing account
// dispatcher with an already authenticated, deliberately stale caller). No new
// production helper is needed to compile the regression tests against stage198.
type credentials199Fixture struct {
	*email197Fixture
	role, username string
	user           identity.User
}

const credentials199OldPassword = "local-email197-password"
const credentials199NewPassword = "local-credentials199-next-password"

func credentials199Local(t *testing.T, role string) *credentials199Fixture {
	t.Helper()
	f := &credentials199Fixture{email197Fixture: email197Local(t), role: role, username: "email197", user: identity.User{ID: 1, Username: "email197", Role: "merchant", AuthVersion: 1}}
	if role == "admin" {
		if _, e := f.p.Exec(t.Context(), `UPDATE console_users SET password_hash=(SELECT password_hash FROM console_users WHERE username='email197' AND role='merchant'),admin_class='operator' WHERE username='admin168' AND role='admin'`); e != nil {
			t.Fatal(e)
		}
		f.username = "admin168"
		f.user = identity.User{ID: 168, Username: f.username, Role: role, AdminClass: "operator"}
		if e := f.p.QueryRow(t.Context(), `SELECT auth_version FROM console_users WHERE username=$1 AND role=$2`, f.username, role).Scan(&f.user.AuthVersion); e != nil {
			t.Fatal(e)
		}
		var e error
		f.token, f.csrf, e = f.s.Sessions.Create(f.user)
		if e != nil {
			t.Fatal(e)
		}
	}
	return f
}

func (f *credentials199Fixture) req(path string, body any) *http.Request {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/console/v1/"+f.role+"/account/"+path, strings.NewReader(string(raw)))
	r.RemoteAddr = "192.0.2.199:12345"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_" + f.role, Value: f.token})
	return r
}

func (f *credentials199Fixture) post(path, password string) *httptest.ResponseRecorder {
	body := map[string]string{"current_password": password}
	if path == "password" {
		body["new_password"] = credentials199NewPassword
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, f.req(path, body))
	return w
}

type credentials199State struct {
	hash, key, mfa         string
	version, sessions      int64
	audits, journalEntries int64
}

func (f *credentials199Fixture) state(t *testing.T) credentials199State {
	t.Helper()
	var v credentials199State
	e := f.p.QueryRow(t.Context(), `SELECT u.password_hash,u.auth_version,
 (SELECT count(*) FROM console_sessions WHERE username=$1 AND role=$2),
 (SELECT count(*) FROM backoffice_audit WHERE resource='account' AND operation IN('password-change','api-key-rotate')),
 (SELECT encode(api_key,'hex') FROM merchants WHERE id=1),
 COALESCE((SELECT row_to_json(m)::text FROM console_mfa m WHERE username=$1 AND role=$2),''),
 (SELECT count(*) FROM journals)
 FROM console_users u WHERE u.username=$1 AND u.role=$2`, f.username, f.role).Scan(&v.hash, &v.version, &v.sessions, &v.audits, &v.key, &v.mfa, &v.journalEntries)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func credentials199SameEffects(t *testing.T, before, after credentials199State) {
	t.Helper()
	if before.hash != after.hash || before.key != after.key || before.audits != after.audits || before.mfa != after.mfa || before.journalEntries != after.journalEntries {
		t.Fatal("rejected credential operation changed password, payment key, audit, MFA, or ledger")
	}
}

func TestCredentials199WrongPasswordIs422AndPreservesSession(t *testing.T) {
	for _, role := range []string{"merchant", "admin"} {
		paths := []string{"password"}
		if role == "merchant" {
			paths = append(paths, "api-key/rotate")
		}
		for _, path := range paths {
			t.Run(role+"/"+path, func(t *testing.T) {
				f := credentials199Local(t, role)
				before := f.state(t)
				w := f.post(path, "incorrect-confirmation199")
				if w.Code != 422 {
					t.Fatalf("incorrect confirmation HTTP=%d; want422 without treating valid login as expired", w.Code)
				}
				if _, ok := f.s.Sessions.Get(f.token, role); !ok {
					t.Fatal("incorrect confirmation revoked the valid original session")
				}
				after := f.state(t)
				credentials199SameEffects(t, before, after)
				if before.version != after.version || before.sessions != after.sessions {
					t.Fatal("incorrect confirmation changed authentication state")
				}
			})
		}
	}
}

func TestCredentials199ProfileReportsActualPasswordStateWithoutCredentialMaterial(t *testing.T) {
	f := credentials199Local(t, "merchant")
	for _, hasPassword := range []bool{true, false} {
		if !hasPassword {
			if _, e := f.p.Exec(t.Context(), `UPDATE console_users SET password_hash='' WHERE username=$1 AND role='merchant'`, f.username); e != nil {
				t.Fatal(e)
			}
			f.user.AuthVersion++
			var e error
			f.token, f.csrf, e = f.s.Sessions.Create(f.user)
			if e != nil {
				t.Fatal(e)
			}
		}
		v := profile198Get(t, f.email197Fixture, "")
		if v["has_pwd"] != hasPassword {
			t.Fatal("profile has_pwd is missing or does not reflect actual password state")
		}
		for _, key := range []string{"password", "password_hash", "salt", "api_key"} {
			if _, exposed := v[key]; exposed {
				t.Fatal("profile returned credential material", key)
			}
		}
	}
	if w := f.post("password", ""); w.Code == 200 {
		t.Fatal("API-key-capable account without a password initialized a new password without recovery proof")
	}
}

func TestCredentials199PasswordChangesAtomicallyRevokeAllOwnSessionsAndPreserveOtherState(t *testing.T) {
	for _, role := range []string{"merchant", "admin"} {
		t.Run(role, func(t *testing.T) {
			f := credentials199Local(t, role)
			otherToken, _, e := f.s.Sessions.Create(f.user)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.p.Exec(t.Context(), `INSERT INTO console_mfa(username,role,secret,last_counter) VALUES($1,$2,'\x1234'::bytea,123)`, f.username, role); e != nil {
				t.Fatal(e)
			}
			before := f.state(t)
			w := f.post("password", credentials199OldPassword)
			if w.Code != 200 {
				t.Fatal("original account could not change its own password", role, w.Code)
			}
			after := f.state(t)
			// The existing DB trigger bumps a changed hash once. Explicit version
			// changes must preserve exactly +1, not accidentally cause a second bump.
			if after.version != before.version+1 || after.sessions != 0 || after.audits != before.audits+1 || !identity.CheckPassword(after.hash, credentials199NewPassword) || identity.CheckPassword(after.hash, credentials199OldPassword) {
				t.Fatal("password replacement, one auth generation, own session revocation, and audit were not one completed result")
			}
			if _, ok := f.s.Sessions.Get(otherToken, role); ok {
				t.Fatal("another old session survived password replacement")
			}
			if after.key != before.key || after.mfa != before.mfa || after.journalEntries != before.journalEntries {
				t.Fatal("password replacement changed payment key, MFA, or ledger")
			}
		})
	}
}

func TestCredentials199StaleOrDelegatedCallerCannotMutateCredentials(t *testing.T) {
	for _, path := range []string{"password", "api-key/rotate"} {
		for _, source := range []string{"memory-session", "deleted-session", "auth-version", "hidden-delegation", "disabled-merchant"} {
			t.Run(path+"/"+source, func(t *testing.T) {
				f := credentials199Local(t, "merchant")
				session, ok := f.s.Sessions.Get(f.token, "merchant")
				if !ok {
					t.Fatal("fixture session missing")
				}
				switch source {
				case "memory-session":
					f.s.Sessions = identity.NewSessions(false)
					var e error
					f.token, f.csrf, e = f.s.Sessions.Create(f.user)
					if e != nil {
						t.Fatal(e)
					}
					session, _ = f.s.Sessions.Get(f.token, "merchant")
				case "deleted-session":
					if _, e := f.p.Exec(t.Context(), `DELETE FROM console_sessions WHERE username=$1 AND role='merchant'`, f.username); e != nil {
						t.Fatal(e)
					}
				case "auth-version":
					if _, e := f.p.Exec(t.Context(), `UPDATE console_users SET auth_version=auth_version+1 WHERE username=$1 AND role='merchant'`, f.username); e != nil {
						t.Fatal(e)
					}
				case "disabled-merchant":
					if _, e := f.p.Exec(t.Context(), `UPDATE merchants SET enabled=false WHERE id=1`); e != nil {
						t.Fatal(e)
					}
				case "hidden-delegation":
					tx, e := f.p.Begin(t.Context())
					if e != nil {
						t.Fatal(e)
					}
					defer tx.Rollback(t.Context())
					f.token, f.csrf, _, e = f.s.Sessions.CreateDelegated(t.Context(), tx, f.user, identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1}, f.channel168Fixture.token)
					if e != nil {
						t.Fatal(e)
					}
					if e = tx.Commit(t.Context()); e != nil {
						t.Fatal(e)
					}
					session, ok = f.s.Sessions.Get(f.token, "merchant")
					if !ok || session.Issuer == nil {
						t.Fatal("delegated fixture missing durable issuer")
					}
					// The transaction must inspect durable provenance even if a caller
					// forgets to propagate the in-memory issuer field.
					session.Issuer = nil
				}
				before := f.state(t)
				body := map[string]string{"current_password": credentials199OldPassword}
				if path == "password" {
					body["new_password"] = credentials199NewPassword
				}
				w := httptest.NewRecorder()
				parts := append([]string{"merchant", "account"}, strings.Split(path, "/")...)
				if !f.s.account(w, f.req(path, body), "merchant", session, parts) || w.Code != 403 {
					t.Fatal("non-original durable caller changed credentials", source, w.Code)
				}
				after := f.state(t)
				credentials199SameEffects(t, before, after)
				if before.version != after.version || before.sessions != after.sessions {
					t.Fatal("rejected caller changed auth generation or sessions")
				}
			})
		}
	}
}

func TestCredentials199LockWaitRechecksPersistentSession(t *testing.T) {
	for _, path := range []string{"password", "api-key/rotate"} {
		for _, change := range []string{"expiry", "revocation", "auth-version"} {
			t.Run(path+"/"+change, func(t *testing.T) {
				f := credentials199Local(t, "merchant")
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				block, e := f.p.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer block.Rollback(context.Background())
				if _, e = block.Exec(ctx, `SELECT username FROM console_users WHERE username='email197' AND role='merchant' FOR UPDATE`); e != nil {
					t.Fatal(e)
				}
				before := f.state(t)
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- f.post(path, credentials199OldPassword) }()
				for {
					var waiting bool
					if e = f.p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))`, block.Conn().PgConn().PID()).Scan(&waiting); e != nil {
						t.Fatal(e)
					}
					if waiting {
						break
					}
					select {
					case early := <-done:
						t.Fatal("credential request never reached controlled row-lock wait", early.Code)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(10 * time.Millisecond):
					}
				}
				if change == "auth-version" {
					_, e = block.Exec(ctx, `UPDATE console_users SET auth_version=auth_version+1 WHERE username='email197' AND role='merchant'`)
				} else {
					sql := `UPDATE console_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE username='email197' AND role='merchant'`
					if change == "revocation" {
						sql = `DELETE FROM console_sessions WHERE username='email197' AND role='merchant'`
					}
					_, e = f.p.Exec(ctx, sql)
				}
				if e != nil {
					t.Fatal(e)
				}
				if e = block.Commit(ctx); e != nil {
					t.Fatal(e)
				}
				select {
				case w := <-done:
					if w.Code != 403 {
						t.Fatal("session revoked/expired during lock wait still authorized credential mutation", change, w.Code)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				credentials199SameEffects(t, before, f.state(t))
			})
		}
	}
}

func TestCredentials199LateExpiryAfterBusinessWritesRollsBack(t *testing.T) {
	for _, point := range []string{"password-audit", "password-session-delete", "md5-audit", "md5-key-update"} {
		t.Run(point, func(t *testing.T) {
			f := credentials199Local(t, "merchant")
			path, event, table, condition, returned := "password", "INSERT", "backoffice_audit", "NEW.operation='password-change'", "NEW"
			switch point {
			case "password-session-delete":
				event, table, condition, returned = "DELETE", "console_sessions", "OLD.role='merchant' AND OLD.subject_id=1", "OLD"
			case "md5-audit":
				path, condition = "api-key/rotate", "NEW.operation='api-key-rotate'"
			case "md5-key-update":
				path, event, table, condition = "api-key/rotate", "UPDATE", "merchants", "OLD.api_key IS DISTINCT FROM NEW.api_key"
			}
			sql := fmt.Sprintf(`CREATE SEQUENCE credentials199_probe; CREATE FUNCTION credentials199_delay() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('credentials199_probe');PERFORM pg_sleep(3);RETURN %s;END$$; CREATE TRIGGER credentials199_delay AFTER %s ON %s FOR EACH ROW WHEN(%s) EXECUTE FUNCTION credentials199_delay(); UPDATE console_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE username='email197' AND role='merchant'`, returned, event, table, condition)
			if _, e := f.p.Exec(t.Context(), sql); e != nil {
				t.Fatal(e)
			}
			before := f.state(t)
			w := f.post(path, credentials199OldPassword)
			var reached bool
			if e := f.p.QueryRow(t.Context(), `SELECT is_called FROM credentials199_probe`).Scan(&reached); e != nil || !reached {
				t.Fatal("controlled last business-write delay was not reached", e)
			}
			if w.Code != 403 {
				t.Fatal("original session expired after business write but operation completed", point, w.Code)
			}
			after := f.state(t)
			credentials199SameEffects(t, before, after)
			if before.version != after.version || before.sessions != after.sessions {
				t.Fatal("late expiry left password generation or self-revocation effects")
			}
		})
	}
}

func TestCredentials199AuditOrSessionDeleteFailureRollsBackPassword(t *testing.T) {
	for _, point := range []string{"audit", "delete"} {
		t.Run(point, func(t *testing.T) {
			f := credentials199Local(t, "merchant")
			event, table, condition, returned := "INSERT", "backoffice_audit", "NEW.operation='password-change'", "NEW"
			if point == "delete" {
				event, table, condition, returned = "DELETE", "console_sessions", "OLD.role='merchant' AND OLD.subject_id=1", "OLD"
			}
			sql := fmt.Sprintf(`CREATE SEQUENCE credentials199_probe; CREATE FUNCTION credentials199_fail() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('credentials199_probe');RAISE EXCEPTION 'synthetic credential199 rollback';RETURN %s;END$$; CREATE TRIGGER credentials199_fail BEFORE %s ON %s FOR EACH ROW WHEN(%s) EXECUTE FUNCTION credentials199_fail()`, returned, event, table, condition)
			if _, e := f.p.Exec(t.Context(), sql); e != nil {
				t.Fatal(e)
			}
			before := f.state(t)
			w := f.post("password", credentials199OldPassword)
			if w.Code < 500 {
				t.Fatal("failed mandatory audit/session revocation reported a completed password change", point, w.Code)
			}
			var reached bool
			if e := f.p.QueryRow(t.Context(), `SELECT is_called FROM credentials199_probe`).Scan(&reached); e != nil || !reached {
				t.Fatal("controlled failure branch not reached", e)
			}
			after := f.state(t)
			credentials199SameEffects(t, before, after)
			if before.version != after.version || before.sessions != after.sessions {
				t.Fatal("failed mandatory effect left a new password generation or deleted session")
			}
		})
	}
}

func TestCredentials199MD5AuditFailureRollsBackAndNeverReturnsKey(t *testing.T) {
	f := credentials199Local(t, "merchant")
	if _, e := f.p.Exec(t.Context(), `CREATE FUNCTION credentials199_fail() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'synthetic credential199 rollback';END$$; CREATE TRIGGER credentials199_fail BEFORE INSERT ON backoffice_audit FOR EACH ROW WHEN(NEW.operation='api-key-rotate') EXECUTE FUNCTION credentials199_fail()`); e != nil {
		t.Fatal(e)
	}
	before := f.state(t)
	w := f.post("api-key/rotate", credentials199OldPassword)
	if w.Code < 500 || strings.Contains(w.Body.String(), `"api_key"`) {
		t.Fatal("failed MD5 audit disclosed a key or reported success", w.Code)
	}
	after := f.state(t)
	credentials199SameEffects(t, before, after)
	if before.version != after.version || before.sessions != after.sessions {
		t.Fatal("failed MD5 audit changed account authentication state")
	}
}

func TestCredentials199FailureBudgetSurvivesBusinessRollbackAndReauthentication(t *testing.T) {
	f := credentials199Local(t, "merchant")
	if _, e := f.p.Exec(t.Context(), `CREATE FUNCTION credentials199_fail() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'synthetic credential199 rollback';END$$; CREATE TRIGGER credentials199_fail BEFORE INSERT ON backoffice_audit FOR EACH ROW WHEN(NEW.operation='password-change') EXECUTE FUNCTION credentials199_fail()`); e != nil {
		t.Fatal(e)
	}
	if w := f.post("password", credentials199OldPassword); w.Code < 500 {
		t.Fatal("controlled business rollback was not reached", w.Code)
	}
	if _, e := f.p.Exec(t.Context(), `DROP TRIGGER credentials199_fail ON backoffice_audit`); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 9; i++ {
		path := "password"
		if i%2 == 0 {
			path = "api-key/rotate"
		}
		if w := f.post(path, "wrong-credentials199-confirmation"); w.Code != 422 {
			t.Fatal("wrong password did not consume one ordinary confirmation attempt", i, w.Code)
		}
	}
	// A new server-side session on the same durable pool must not reset budgets.
	f.s.Sessions = identity.NewPersistentSessions(f.p, false)
	var e error
	f.token, f.csrf, e = f.s.Sessions.Create(f.user)
	if e != nil {
		t.Fatal(e)
	}
	before := f.state(t)
	if w := f.post("password", credentials199OldPassword); w.Code != 429 {
		t.Fatal("rollback or new login reset persistent confirmation budget", w.Code)
	}
	if w := f.post("api-key/rotate", credentials199OldPassword); w.Code != 429 {
		t.Fatal("MD5 rotation bypassed shared confirmation budget", w.Code)
	}
	credentials199SameEffects(t, before, f.state(t))
}

func TestCredentials199ConcurrentPasswordChangesHaveOneAtomicWinner(t *testing.T) {
	f := credentials199Local(t, "merchant")
	before := f.state(t)
	done := make(chan int, 2)
	for range 2 {
		go func() { done <- f.post("password", credentials199OldPassword).Code }()
	}
	success := 0
	for range 2 {
		status := <-done
		if status == 200 {
			success++
		} else if status != 401 && status != 403 {
			t.Fatal("concurrent password request returned unexpected status", status)
		}
	}
	after := f.state(t)
	if success != 1 || after.version != before.version+1 || after.audits != before.audits+1 || after.sessions != 0 || !identity.CheckPassword(after.hash, credentials199NewPassword) {
		t.Fatal("concurrent old-password confirmations produced more than one credential outcome")
	}
	if after.key != before.key || after.mfa != before.mfa || after.journalEntries != before.journalEntries {
		t.Fatal("concurrent password confirmations changed payment, MFA, or ledger state")
	}
}

func TestCredentials199MD5SuccessReturnsOnlyCommittedKeyAndPreservesPassword(t *testing.T) {
	f := credentials199Local(t, "merchant")
	before := f.state(t)
	w := f.post("api-key/rotate", credentials199OldPassword)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("MD5 rotation HTTP result", w.Code)
	}
	var v struct {
		Code int
		Data struct {
			APIKey string `json:"api_key"`
		}
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || v.Code != 0 || len(v.Data.APIKey) < 24 {
		t.Fatal("committed key missing from one-time success response", e)
	}
	m, e := f.s.Store.Merchant(t.Context(), 1)
	if e != nil || m.APIKey != v.Data.APIKey {
		t.Fatal("response key is not the committed encrypted payment credential", e)
	}
	after := f.state(t)
	if after.key == before.key || after.hash != before.hash || after.version != before.version || after.sessions != before.sessions || after.audits != before.audits+1 || after.mfa != before.mfa || after.journalEntries != before.journalEntries {
		t.Fatal("MD5 rotation changed unrelated password, sessions, MFA, or ledger state")
	}
	// The original persisted session source exists; hashing is deliberately test
	// local, matching the cookie's real durable lookup rather than a fabricated ID.
	h := sha256.Sum256([]byte(f.token))
	var exists bool
	if e = f.p.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM console_sessions WHERE token_hash=$1)`, h[:]).Scan(&exists); e != nil || !exists {
		t.Fatal("MD5 rotation unexpectedly revoked its original login", e)
	}
}
