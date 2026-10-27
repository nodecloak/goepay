package server

import (
	"goepay/internal/identity"
	"strings"
	"testing"
)

func email197Target(t *testing.T, f *email197Fixture, id string) string {
	t.Helper()
	f.start(t, id)
	email197Data(t, f.request("POST", "/"+id+"/new-code", map[string]any{"email": "target197@example.test"}))
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.messages[len(f.messages)-1].code
}
func TestEmail197DeletedCompletionCannotTurnChangedIdentityIntoLegacyClaim(t *testing.T) {
	f := email197Local(t)
	id := strings.Repeat("u", 43)
	code := email197Target(t, f, id)
	email197Data(t, f.request("POST", "/"+id+"/complete", map[string]any{"code": code}))
	var e error
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: 1, Username: "email197", Role: "merchant", AuthVersion: 2})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(t.Context(), `ALTER TABLE email_binding_events197 DISABLE TRIGGER USER;DELETE FROM email_binding_events197;ALTER TABLE email_binding_events197 ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if w := f.request("GET", "", nil); w.Code != 503 {
		t.Fatal("lost completion source restored legacy recovery authority", w.Code)
	}
}
func TestEmail197FinalSessionExpiryRollsBackBindingProofAndOwnRevocation(t *testing.T) {
	f := email197Local(t)
	id := strings.Repeat("v", 43)
	code := email197Target(t, f, id)
	if _, e := f.p.Exec(t.Context(), `CREATE SEQUENCE email197_final_probe;CREATE FUNCTION email197_final_delay() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('email197_final_probe');PERFORM pg_sleep(3);RETURN NEW;END$$;CREATE TRIGGER email197_final_delay BEFORE INSERT ON backoffice_audit FOR EACH ROW WHEN(NEW.operation='verified-email-change')EXECUTE FUNCTION email197_final_delay();UPDATE console_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE role='merchant' AND subject_id=1`); e != nil {
		t.Fatal(e)
	}
	if w := f.request("POST", "/"+id+"/complete", map[string]any{"code": code}); w.Code != 403 {
		t.Fatal("expired final session completed binding", w.Code)
	}
	var reached bool
	var effects, version, sessions int
	if e := f.p.QueryRow(t.Context(), `SELECT is_called FROM email197_final_probe`).Scan(&reached); e != nil || !reached {
		t.Fatal("final audit write was not reached", e)
	}
	if e := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*)FROM enrollment_emails)+(SELECT count(*)FROM email_binding_events197 WHERE kind='complete')+(SELECT count(*)FROM backoffice_audit WHERE operation='verified-email-change')+(SELECT count(*)FROM enrollment_challenges WHERE consumed_at IS NOT NULL),(SELECT auth_version FROM console_users WHERE username='email197' AND role='merchant'),(SELECT count(*)FROM console_sessions WHERE role='merchant' AND subject_id=1)`).Scan(&effects, &version, &sessions); e != nil || effects != 0 || version != 1 || sessions != 1 {
		t.Fatal("expired completion left claim/proof/version effects", effects, version, sessions, e)
	}
}
func TestEmail197FiveIncorrectProofsRemainExhaustedAcrossRequests(t *testing.T) {
	f := email197Local(t)
	id := strings.Repeat("w", 43)
	code := email197Target(t, f, id)
	for range 5 {
		if w := f.request("POST", "/"+id+"/complete", map[string]any{"code": "0000000"}); w.Code != 422 {
			t.Fatal("bad proof not rejected", w.Code)
		}
	}
	if w := f.request("POST", "/"+id+"/complete", map[string]any{"code": code}); w.Code != 422 {
		t.Fatal("exhausted proof regained authority", w.Code)
	}
	var attempts, claims int
	if e := f.p.QueryRow(t.Context(), `SELECT (SELECT attempts FROM enrollment_challenges WHERE purpose='contact-new'),(SELECT count(*)FROM enrollment_emails)`).Scan(&attempts, &claims); e != nil || attempts != 5 || claims != 0 {
		t.Fatal("failed attempts rolled back or established identity", attempts, claims, e)
	}
}
