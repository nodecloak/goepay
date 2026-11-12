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

func adminMutationRequest(ctx context.Context, f channel168Fixture, operation string, id int64) *httptest.ResponseRecorder {
	method, path, body := "POST", "/console/v1/admin/resources/admin-users", `{"username":"new-authority-admin","display_name":"New Admin","password":"synthetic-new-admin-password","admin_class":"operator"}`
	switch operation {
	case "update":
		method, path, body = "PATCH", fmt.Sprintf("%s/%d", path, id), `{"display_name":"Changed Admin"}`
	case "delete":
		method, path, body = "DELETE", fmt.Sprintf("%s/%d", path, id), ""
	case "security-reset":
		path, body = fmt.Sprintf("/console/v1/admin/admin-users/%d/security-reset", id), `{"disable_totp":true,"reset_funds_credential":true}`
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func adminMutationTarget(t *testing.T, f channel168Fixture) int64 {
	t.Helper()
	if _, err := f.p.Exec(t.Context(), `INSERT INTO console_mfa(username,role,secret,last_counter) VALUES('backup168','admin','\x1234'::bytea,3); INSERT INTO admin_funds_credentials(username,password_hash) VALUES('backup168','$argon2id$v=19$m=1,t=1,p=1$c2FsdHNhbHQ$aaaa')`); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := f.p.QueryRow(t.Context(), "SELECT id FROM console_users WHERE username='backup168' AND role='admin'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// Exclude only the actor that the test deliberately revokes. All target rows,
// credentials and audit rows must remain identical after a rejected mutation.
func adminMutationDigest(t *testing.T, f channel168Fixture) string {
	t.Helper()
	var digest string
	if err := f.p.QueryRow(t.Context(), `SELECT md5(jsonb_build_array(
		(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM console_users u WHERE username<>'admin168'),
		(SELECT jsonb_agg(to_jsonb(m) ORDER BY username,role) FROM console_mfa m),
		(SELECT jsonb_agg(to_jsonb(c) ORDER BY username) FROM admin_funds_credentials c),
		(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM backoffice_audit a))::text)`).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestAdminMutationsRejectAuthorityRevokedWhileWaiting(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete", "security-reset"} {
		for _, change := range []string{"demote", "logout", "memory-logout"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				f := channel168Local(t)
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				id := adminMutationTarget(t, f)
				if change == "memory-logout" {
					f.s.Sessions = identity.NewSessions(false)
					var err error
					f.token, f.csrf, err = f.s.Sessions.Create(identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1})
					if err != nil {
						t.Fatal(err)
					}
				}
				// An insert may also wait on a trigger/constraint lock. This gate
				// proves the original create path's race before its lifecycle lock
				// is introduced, without blocking the independent revoking UPDATE.
				if operation == "create" {
					if _, err := f.p.Exec(ctx, `CREATE FUNCTION admin_create_wait() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM pg_advisory_xact_lock(74973232); RETURN NEW; END$$; CREATE TRIGGER admin_create_wait BEFORE INSERT ON console_users FOR EACH ROW EXECUTE FUNCTION admin_create_wait()`); err != nil {
						t.Fatal(err)
					}
				}
				before := adminMutationDigest(t, f)
				block, err := f.p.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer block.Rollback(context.Background())
				if _, err = block.Exec(ctx, "SELECT pg_advisory_xact_lock(74973232)"); err != nil {
					t.Fatal(err)
				}
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- adminMutationRequest(ctx, f, operation, id) }()
				for {
					var waiting bool
					if err = f.p.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))", block.Conn().PgConn().PID()).Scan(&waiting); err != nil {
						t.Fatal(err)
					}
					if waiting {
						break
					}
					select {
					case w := <-done:
						t.Fatalf("mutation did not wait: HTTP %d %s", w.Code, w.Body.String())
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(10 * time.Millisecond):
					}
				}
				if change == "demote" {
					if _, err = f.p.Exec(ctx, "UPDATE console_users SET admin_class='operator' WHERE username='admin168'"); err != nil {
						t.Fatal(err)
					}
				} else {
					f.s.Sessions.Delete(f.token)
				}
				if err = block.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case w := <-done:
					if w.Code != 403 {
						t.Errorf("revoked administrator mutation: HTTP %d want 403: %s", w.Code, w.Body.String())
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if after := adminMutationDigest(t, f); after != before {
					t.Error("revoked administrator changed target, security credentials or audit")
				}
			})
		}
	}
}

func TestAdminMutationsRejectSessionExpiryAfterAudit(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete", "security-reset"} {
		t.Run(operation, func(t *testing.T) {
			f := channel168Local(t)
			id := adminMutationTarget(t, f)
			before := adminMutationDigest(t, f)
			if _, err := f.p.Exec(t.Context(), `CREATE SEQUENCE admin_audit_entered; CREATE FUNCTION admin_audit_wait() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='admin-users' THEN PERFORM nextval('admin_audit_entered'); PERFORM pg_sleep(1.3); END IF; RETURN NEW; END$$; CREATE TRIGGER admin_audit_wait BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION admin_audit_wait(); UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='admin168'`); err != nil {
				t.Fatal(err)
			}
			if w := adminMutationRequest(t.Context(), f, operation, id); w.Code != 403 {
				t.Errorf("expired administrator mutation: HTTP %d want 403: %s", w.Code, w.Body.String())
			}
			var entered bool
			if err := f.p.QueryRow(t.Context(), "SELECT is_called FROM admin_audit_entered").Scan(&entered); err != nil || !entered {
				t.Fatal("audit expiry gate was not reached", err)
			}
			if after := adminMutationDigest(t, f); after != before {
				t.Error("expired administrator changed target, security credentials or audit")
			}
		})
	}
}

func TestAdminMutationsAllowUnchangedAuthority(t *testing.T) {
	f := channel168Local(t)
	id := adminMutationTarget(t, f)
	for _, operation := range []string{"create", "update", "security-reset", "delete"} {
		want := 200
		if operation == "create" {
			want = 201
		}
		if w := adminMutationRequest(t.Context(), f, operation, id); w.Code != want {
			t.Fatalf("authorized %s: HTTP %d want %d: %s", operation, w.Code, want, w.Body.String())
		}
	}
	var audits int
	if err := f.p.QueryRow(t.Context(), "SELECT count(*) FROM backoffice_audit WHERE resource='admin-users'").Scan(&audits); err != nil || audits != 4 {
		t.Fatal("authorized mutations did not each persist one audit", audits, err)
	}
}

func TestAdminSelfCredentialEditsCommitAndInvalidateOriginalSession(t *testing.T) {
	for _, change := range []string{"password", "login-name"} {
		t.Run(change, func(t *testing.T) {
			f := channel168Local(t)
			var id int64
			if err := f.p.QueryRow(t.Context(), "SELECT id FROM console_users WHERE username='admin168' AND role='admin'").Scan(&id); err != nil {
				t.Fatal(err)
			}
			body := `{"password":"replacement-self-admin-password"}`
			if change == "login-name" {
				body = `{"username":"renamed-self-admin","expected_username":"admin168"}`
			}
			r := httptest.NewRequest("PATCH", fmt.Sprintf("/console/v1/admin/resources/admin-users/%d", id), strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", f.csrf)
			r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
			w := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("authorized self credential edit: HTTP %d: %s", w.Code, w.Body.String())
			}
			var username, login, hash string
			var version, audits int64
			if err := f.p.QueryRow(t.Context(), `SELECT username,COALESCE(login_name,username),password_hash,auth_version,(SELECT count(*) FROM backoffice_audit WHERE resource='admin-users' AND operation='update') FROM console_users WHERE id=$1`, id).Scan(&username, &login, &hash, &version, &audits); err != nil {
				t.Fatal(err)
			}
			if username != "admin168" || version != 2 || audits != 1 {
				t.Fatal("self edit changed canonical identity or lost epoch/audit", username, version, audits)
			}
			if change == "login-name" && login != "renamed-self-admin" {
				t.Fatal("login alias did not persist", login)
			}
			if change == "password" && !identity.CheckPassword(hash, "replacement-self-admin-password") {
				t.Fatal("password did not persist")
			}
			if _, valid := f.s.Sessions.Get(f.token, "admin"); valid {
				t.Fatal("self credential edit retained the original session")
			}
		})
	}
}
