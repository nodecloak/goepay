package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func mail195Save(t *testing.T, f channel168Fixture, state map[string]any, host, user, password string, status int, keep bool) map[string]any {
	t.Helper()
	config := map[string]string{"host": host, "port": "465", "username": user}
	if !keep {
		config["password"] = password
	}
	input := map[string]any{"name": "smtp", "status": status, "config": config, "expected_fingerprint": state["fingerprint"]}
	if keep {
		input["keep_password"] = true
	}
	body, _ := json.Marshal(input)
	return mail195Data(t, mail195Request(f, "PATCH", "/smtp", string(body)))
}

func TestMailAccounts195EndpointChangeCannotReuseOldPasswordAndMalformedInputDenied(t *testing.T) {
	f := channel168Local(t)
	state := mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
	state = mail195Save(t, f, state, "first.example.invalid", "sender195@example.invalid", "synthetic195-account-password", 1, false)
	for _, config := range []map[string]any{
		{"host": "second.example.invalid", "port": "465", "username": "sender195@example.invalid"},
		{"host": "first.example.invalid", "port": "587", "username": "sender195@example.invalid"},
		{"host": "first.example.invalid", "port": "465", "username": "other195@example.invalid"},
	} {
		body, _ := json.Marshal(map[string]any{"name": "smtp", "status": 1, "config": config, "keep_password": true, "expected_fingerprint": state["fingerprint"]})
		if w := mail195Request(f, "PATCH", "/smtp", string(body)); w.Code != 422 {
			t.Fatal("old secret sent to changed endpoint/account", w.Code)
		}
	}
	base := `{"name":"smtp","status":1,"config":{"host":"first.example.invalid","port":"465","username":"sender195@example.invalid","password":"synthetic195-account-password"},"expected_fingerprint":"` + state["fingerprint"].(string) + `"}`
	for _, raw := range []string{
		strings.Replace(base, `"status":1`, `"status":2`, 1),
		strings.Replace(base, `"status":1`, `"status":true`, 1),
		strings.Replace(base, `"465"`, `"0465"`, 1),
		strings.Replace(base, `"host":"first.example.invalid"`, `"host":"https://first.example.invalid"`, 1),
		strings.Replace(base, `"host":"first.example.invalid"`, `"host":"first.example.invalid","host":"second.example.invalid"`, 1),
		strings.Replace(base, `"port":"465"`, `"port":"465","security":"plain"`, 1),
	} {
		if w := mail195Request(f, "PATCH", "/smtp", raw); w.Code != 400 && w.Code != 422 {
			t.Fatal("invalid/ambiguous SMTP fields accepted", w.Code)
		}
	}
	if got := mail195Data(t, mail195Request(f, "GET", "/smtp", "")); got["fingerprint"] != state["fingerprint"] {
		t.Fatal("rejected changes mutated account")
	}
}

func TestMailAccounts195FinalSessionExpiryRollsBackEncryptedRevisionWitnessAndAudit(t *testing.T) {
	f := channel168Local(t)
	state := mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
	if _, e := f.p.Exec(t.Context(), `CREATE SEQUENCE mail195_final_probe;CREATE FUNCTION mail195_delay()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('mail195_final_probe');PERFORM pg_sleep(3);RETURN NEW;END$$;CREATE TRIGGER mail195_delay_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW WHEN(NEW.resource='mail-account195')EXECUTE FUNCTION mail195_delay();UPDATE console_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE role='admin'`); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"name": "smtp", "status": 1, "config": map[string]string{"host": "smtp.example.invalid", "port": "465", "username": "sender195@example.invalid", "password": "synthetic195-final"}, "expected_fingerprint": state["fingerprint"]})
	if w := mail195Request(f, "PATCH", "/smtp", string(body)); w.Code != 403 {
		t.Fatal("expired final session committed account", w.Code)
	}
	var entered bool
	var effects int
	if e := f.p.QueryRow(t.Context(), `SELECT is_called FROM mail195_final_probe`).Scan(&entered); e != nil || !entered {
		t.Fatal("last write not reached", entered, e)
	}
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM mail_account_revisions195)+(SELECT count(*)FROM mail_account_witnesses195)+(SELECT count(*)FROM backoffice_audit WHERE resource='mail-account195')+(SELECT revision::int FROM mail_account_head195)`).Scan(&effects); e != nil || effects != 0 {
		t.Fatal("expired save left effects", effects, e)
	}
}

func TestMailAccounts195ChangedCiphertextAndLostSourceNeverRestoreDeploymentFallback(t *testing.T) {
	for _, mutation := range []string{
		`ALTER TABLE mail_account_revisions195 DISABLE TRIGGER ALL;UPDATE mail_account_revisions195 SET facts=jsonb_set(facts,'{Status}','0'::jsonb);ALTER TABLE mail_account_revisions195 ENABLE TRIGGER ALL`,
		`ALTER TABLE mail_account_head195 DISABLE TRIGGER USER;DELETE FROM mail_account_head195;ALTER TABLE mail_account_head195 ENABLE TRIGGER USER`,
		`DROP TABLE mail_account_head195`,
	} {
		t.Run(mutation[:15], func(t *testing.T) {
			f := channel168Local(t)
			state := mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
			mail195Save(t, f, state, "smtp.example.invalid", "sender195@example.invalid", "synthetic195-source", 1, false)
			if _, e := f.p.Exec(t.Context(), mutation); e != nil {
				t.Fatal(e)
			}
			if w := mail195Request(f, "GET", "/smtp", ""); w.Code != 503 {
				t.Fatal("unverifiable account restored defaults", w.Code)
			}
		})
	}
}
