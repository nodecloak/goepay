package server

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/backoffice"
	"goepay/internal/identity"
)

func loginName245Request(f *credentials199Fixture, method string, body any) *httptest.ResponseRecorder {
	r := f.req("login-name", body)
	r.Method = method
	if method == "GET" {
		r.Header.Del("X-CSRF-Token")
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func loginName245Body(username, expected, password string) map[string]string {
	return map[string]string{"username": username, "expected_username": expected, "current_password": password}
}

func loginName245Login(f *credentials199Fixture, username, password, code string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]string{"username": username, "password": password, "code": code})
	r := httptest.NewRequest("POST", "/console/v1/admin/login", strings.NewReader(string(raw)))
	r.RemoteAddr = "192.0.2.245:12345"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func loginName245Code(t *testing.T, encoded string) string {
	t.Helper()
	secret, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(encoded)
	if e != nil {
		t.Fatal(e)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(time.Now().Unix()/30))
	h := hmac.New(sha1.New, secret)
	h.Write(counter[:])
	digest := h.Sum(nil)
	offset := digest[len(digest)-1] & 15
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(digest[offset:offset+4])&0x7fffffff)%1000000)
}

func sha256Token245(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func TestAdminLoginName245RenamePreservesCanonicalMFAFundsAndRevokesSessions(t *testing.T) {
	f := credentials199Local(t, "admin")
	ctx := t.Context()
	var e error
	f.s.MFA, e = identity.NewMFA(f.p, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	enrollment, e := f.s.MFA.Prepare(ctx, f.username, "admin")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(ctx, `UPDATE console_mfa SET secret=pending_secret,pending_secret=NULL,pending_until=NULL,last_counter=-1 WHERE username=$1 AND role='admin'`, f.username); e != nil {
		t.Fatal(e)
	}
	fundsHash, e := identity.HashPassword("synthetic-funds245-password")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(ctx, `INSERT INTO admin_funds_credentials(username,password_hash) VALUES($1,$2)`, f.username, fundsHash); e != nil {
		t.Fatal(e)
	}
	secondToken, _, e := f.s.Sessions.Create(f.user)
	if e != nil {
		t.Fatal(e)
	}
	before := f.state(t)
	metadata := email197Data(t, loginName245Request(f, "GET", nil))
	if metadata["username"] != f.username || metadata["auth_version"] != fmt.Sprint(before.version) {
		t.Fatal("login metadata", metadata)
	}
	result := email197Data(t, loginName245Request(f, "POST", loginName245Body("renamed245", f.username, credentials199OldPassword)))
	if result["username"] != "renamed245" || result["reauthenticate"] != true {
		t.Fatal("rename response", result)
	}
	after := f.state(t)
	credentials199SameEffects(t, before, after)
	if after.version != before.version+1 || after.sessions != 0 {
		t.Fatal("rename did not bump epoch exactly once and revoke all sessions", before.version, after.version, after.sessions)
	}
	for _, token := range []string{f.token, secondToken} {
		if _, ok := f.s.Sessions.Get(token, "admin"); ok {
			t.Fatal("renamed administrator retained an old session")
		}
	}
	var canonical, effective, storedFundsHash, audit string
	var fundsEpoch, audits int64
	if e = f.p.QueryRow(ctx, `SELECT u.username,COALESCE(u.login_name,u.username),c.password_hash,c.credential_epoch,(SELECT count(*) FROM backoffice_audit WHERE resource='account' AND operation='login-name-change'),(SELECT changes::text FROM backoffice_audit WHERE resource='account' AND operation='login-name-change') FROM console_users u JOIN admin_funds_credentials c ON c.username=u.username WHERE u.username=$1 AND u.role='admin'`, f.username).Scan(&canonical, &effective, &storedFundsHash, &fundsEpoch, &audits, &audit); e != nil {
		t.Fatal(e)
	}
	if canonical != f.username || effective != "renamed245" || storedFundsHash != fundsHash || fundsEpoch != 1 || audits != 1 || !strings.Contains(audit, "renamed245") || strings.Contains(audit, credentials199OldPassword) || strings.Contains(audit, "$argon2") {
		t.Fatal("canonical credential or redacted audit changed", canonical, effective, fundsEpoch, audits)
	}
	if w := loginName245Login(f, f.username, credentials199OldPassword, loginName245Code(t, enrollment.Secret)); w.Code != 401 {
		t.Fatal("old login name still accepted", w.Code)
	}
	if w := loginName245Login(f, "renamed245", credentials199OldPassword, ""); w.Code != 401 {
		t.Fatal("renamed login bypassed canonical MFA", w.Code)
	}
	w := loginName245Login(f, "renamed245", credentials199OldPassword, loginName245Code(t, enrollment.Secret))
	if w.Code != 200 {
		t.Fatal("new login and canonical MFA failed", w.Code, w.Body.String())
	}
	var newToken string
	for _, c := range w.Result().Cookies() {
		if c.Name == "goepay_admin" {
			newToken = c.Value
		}
	}
	session, ok := f.s.Sessions.Get(newToken, "admin")
	if !ok || session.User.Username != f.username || session.User.AuthVersion != after.version {
		t.Fatal("new login did not retain canonical session identity")
	}
	status, e := identity.NewFundsAuthorization(f.p, f.s.MFA).Status(ctx, identity.FundsActor{User: session.User, SessionToken: newToken, ClientIP: "192.0.2.245"})
	if e != nil || !status.Configured || status.CredentialEpoch != 1 || !status.TOTPRequired {
		t.Fatal("renamed login lost funds/MFA credentials", status, e)
	}
	var lastUsername string
	if e = f.p.QueryRow(ctx, `SELECT username FROM console_login_audit WHERE role='admin' AND success ORDER BY id DESC LIMIT 1`).Scan(&lastUsername); e != nil || lastUsername != f.username {
		t.Fatal("successful login audit lost canonical account", lastUsername, e)
	}
	service := backoffice.New(f.p, f.s.Store)
	var id int64
	if e = f.p.QueryRow(ctx, `SELECT id FROM console_users WHERE username=$1 AND role='admin'`, f.username).Scan(&id); e != nil {
		t.Fatal(e)
	}
	item, e := service.Get(ctx, backoffice.Principal{Role: "admin", AdminClass: "super", Username: "backup168", SubjectID: 169}, "admin-users", id)
	if e != nil || item.Fields["username"] != "renamed245" || item.Fields["last_login_at"] == nil || item.Fields["totp_enabled"] != true {
		t.Fatal("effective directory or canonical metadata lost", item, e)
	}
}

func TestAdminLoginName245RejectedInputsPreserveAccountAndBudgetWrongPasswords(t *testing.T) {
	f := credentials199Local(t, "admin")
	before := f.state(t)
	for _, tc := range []struct {
		name string
		body map[string]string
		want int
	}{
		{"invalid", loginName245Body("bad name", f.username, credentials199OldPassword), 422},
		{"short", loginName245Body("ab", f.username, credentials199OldPassword), 422},
		{"unicode", loginName245Body("账号245", f.username, credentials199OldPassword), 422},
		{"unknown", map[string]string{"username": "valid245", "current_password": credentials199OldPassword, "expected_username": f.username, "role": "super"}, 400},
		{"missing", map[string]string{"username": "valid245", "current_password": credentials199OldPassword}, 422},
		{"wrong-password", loginName245Body("valid245", f.username, "wrong-password"), 422},
		{"duplicate", loginName245Body("backup168", f.username, credentials199OldPassword), 409},
		{"stale", loginName245Body("valid245", "old-login245", credentials199OldPassword), 409},
		{"unchanged", loginName245Body(f.username, f.username, credentials199OldPassword), 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := loginName245Request(f, "POST", tc.body)
			if w.Code != tc.want {
				t.Fatal("validation status", w.Code, tc.want, w.Body.String())
			}
			after := f.state(t)
			credentials199SameEffects(t, before, after)
			if after.version != before.version || after.sessions != before.sessions {
				t.Fatal("rejected rename revoked or changed account")
			}
		})
	}
	var attempts int
	if e := f.p.QueryRow(t.Context(), `SELECT max(attempts) FROM console_login_throttle`).Scan(&attempts); e != nil || attempts < 4 {
		t.Fatal("rejected credential checks escaped persistent budget", attempts, e)
	}
	var audits int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*) FROM backoffice_audit WHERE operation='login-name-change'`).Scan(&audits); e != nil || audits != 0 {
		t.Fatal("rejected rename recorded success", audits, e)
	}
}

func TestAdminLoginName245OriginalSessionAndRestrictedOperatorBoundaries(t *testing.T) {
	for _, source := range []string{"merchant", "deleted-session", "memory-session", "hidden-delegation", "csrf"} {
		t.Run(source, func(t *testing.T) {
			role := "admin"
			if source == "merchant" {
				role = "merchant"
			}
			f := credentials199Local(t, role)
			session, _ := f.s.Sessions.Get(f.token, role)
			r := f.req("login-name", loginName245Body("next245", f.username, credentials199OldPassword))
			switch source {
			case "deleted-session":
				if _, e := f.p.Exec(t.Context(), `DELETE FROM console_sessions WHERE username=$1 AND role=$2`, f.username, role); e != nil {
					t.Fatal(e)
				}
			case "memory-session":
				f.s.Sessions = identity.NewSessions(false)
				var e error
				f.token, f.csrf, e = f.s.Sessions.Create(f.user)
				if e != nil {
					t.Fatal(e)
				}
				session, _ = f.s.Sessions.Get(f.token, role)
				r = f.req("login-name", loginName245Body("next245", f.username, credentials199OldPassword))
			case "hidden-delegation":
				h := sha256Token245(f.token)
				if _, e := f.p.Exec(t.Context(), `INSERT INTO console_session_issuers(token_hash,issuer_session_hash,issuer_username,issuer_id,issuer_auth_version) VALUES($1,$2,'backup168',169,1)`, h, sha256Token245(f.channel168Fixture.token)); e != nil {
					t.Fatal(e)
				}
			case "csrf":
				r.Header.Del("X-CSRF-Token")
			}
			w := httptest.NewRecorder()
			// Invoke with the stale caller as well as the real durable backing, so
			// outer middleware cannot mask a missing transaction-time check.
			f.s.account(w, r, role, session, []string{role, "account", "login-name"})
			if w.Code != 403 {
				t.Fatal("non-original caller accepted", w.Code, w.Body.String())
			}
		})
	}
	f := credentials199Local(t, "admin")
	if _, e := f.p.Exec(t.Context(), `UPDATE console_users SET permissions='[]' WHERE username=$1 AND role='admin'`, f.username); e != nil {
		t.Fatal(e)
	}
	if w := loginName245Request(f, "GET", nil); w.Code != 200 {
		t.Fatal("restricted operator cannot read own login name", w.Code)
	}
	if w := loginName245Request(f, "POST", loginName245Body("restricted245", f.username, credentials199OldPassword)); w.Code != 200 {
		t.Fatal("restricted operator cannot rename own login", w.Code, w.Body.String())
	}
}

func TestAdminLoginName245MandatoryRevocationAuditAndDeadlineRollback(t *testing.T) {
	for _, failure := range []string{"session-delete", "audit", "late-expiry"} {
		t.Run(failure, func(t *testing.T) {
			f := credentials199Local(t, "admin")
			ctx := t.Context()
			statement := `CREATE FUNCTION block_login245() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$;
CREATE TRIGGER block_login245 BEFORE DELETE ON console_sessions FOR EACH ROW EXECUTE FUNCTION block_login245()`
			if failure == "audit" {
				statement = `CREATE FUNCTION block_login245() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit failure'; END $$;
CREATE TRIGGER block_login245 BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION block_login245()`
			} else if failure == "late-expiry" {
				statement = `CREATE FUNCTION block_login245() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(2); RETURN NEW; END $$;
CREATE TRIGGER block_login245 BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION block_login245()`
				if _, e := f.p.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1500 milliseconds' WHERE username=$1 AND role='admin'`, f.username); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := f.p.Exec(ctx, statement); e != nil {
				t.Fatal(e)
			}
			before := f.state(t)
			w := loginName245Request(f, "POST", loginName245Body("rollback245", f.username, credentials199OldPassword))
			want := 503
			if failure == "late-expiry" {
				want = 403
			}
			if w.Code != want {
				t.Fatal("failed mandatory security write returned success", w.Code, want, w.Body.String())
			}
			after := f.state(t)
			credentials199SameEffects(t, before, after)
			if before.version != after.version || before.sessions != after.sessions {
				t.Fatal("failed rename retained partial session or epoch change")
			}
			var effective string
			var audits int
			if e := f.p.QueryRow(ctx, `SELECT COALESCE(login_name,username),(SELECT count(*) FROM backoffice_audit WHERE operation='login-name-change') FROM console_users WHERE username=$1 AND role='admin'`, f.username).Scan(&effective, &audits); e != nil || effective != f.username || audits != 0 {
				t.Fatal("failed rename did not roll back login and audit", effective, audits, e)
			}
		})
	}
}
