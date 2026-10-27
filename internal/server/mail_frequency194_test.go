package server

import (
	"encoding/json"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mailFrequency194Request(f channel168Fixture, method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/settings/mail-frequency", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func TestMailFrequency194OriginalFieldsCASAndNoFinancialOrMessageEffects(t *testing.T) {
	f := channel168Local(t)
	w := mailFrequency194Request(f, "GET", "")
	if w.Code != 200 {
		t.Fatal("original mail frequency settings missing", w.Code)
	}
	state := channel168Data(t, w)
	for key, want := range map[string]string{"mail_interval_email": "60", "mail_interval_ip": "60", "mail_daily_email": "10", "mail_daily_ip": "20"} {
		if state[key] != want {
			t.Fatal("original field/default missing", key, state)
		}
	}
	if state["configured"] != false {
		t.Fatal("upgrade silently took over legacy rate policy", state)
	}
	body, _ := json.Marshal(map[string]any{"mail_interval_email": "120", "mail_interval_ip": "30", "mail_daily_email": "5", "mail_daily_ip": "8", "expected_fingerprint": state["fingerprint"]})
	w = mailFrequency194Request(f, "PATCH", string(body))
	if w.Code != 200 {
		t.Fatal("protected frequency save missing", w.Code, w.Body.String())
	}
	saved := channel168Data(t, w)
	if saved["configured"] != true || saved["mail_daily_email"] != "5" || saved["fingerprint"] == state["fingerprint"] {
		t.Fatal("frequency save not confirmed", saved)
	}
	if w = mailFrequency194Request(f, "PATCH", string(body)); w.Code != 409 {
		t.Fatal("stale mail frequency accepted", w.Code)
	}
	var effects int
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM orders)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)+(SELECT count(*)FROM enrollment_challenges)`).Scan(&effects); e != nil || effects != 0 {
		t.Fatal("frequency config sent a message or touched money", effects, e)
	}
}

func TestMailFrequency194FinalSessionExpiryRollsBackPolicyWitnessAndAudit(t *testing.T) {
	f := channel168Local(t)
	state := channel168Data(t, mailFrequency194Request(f, "GET", ""))
	body, _ := json.Marshal(map[string]any{"mail_interval_email": "60", "mail_interval_ip": "60", "mail_daily_email": "10", "mail_daily_ip": "20", "expected_fingerprint": state["fingerprint"]})
	if _, e := f.p.Exec(t.Context(), `CREATE SEQUENCE mail194_final_probe;CREATE FUNCTION mail194_delay()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('mail194_final_probe');PERFORM pg_sleep(3);RETURN NEW;END$$;CREATE TRIGGER mail194_delay_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW WHEN(NEW.resource='mail-frequency194')EXECUTE FUNCTION mail194_delay();UPDATE console_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE role='admin'`); e != nil {
		t.Fatal(e)
	}
	if w := mailFrequency194Request(f, "PATCH", string(body)); w.Code != 403 {
		t.Fatal("expired session committed policy", w.Code, w.Body.String())
	}
	var entered bool
	if e := f.p.QueryRow(t.Context(), `SELECT is_called FROM mail194_final_probe`).Scan(&entered); e != nil || !entered {
		t.Fatal("final audit delay not reached", entered, e)
	}
	var effects int
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM mail_rate_policies194)+(SELECT count(*)FROM mail_rate_witnesses194)+(SELECT count(*)FROM backoffice_audit WHERE resource='mail-frequency194')+(SELECT revision::int FROM mail_rate_head194)`).Scan(&effects); e != nil || effects != 0 {
		t.Fatal("expired policy save leaked committed effects", effects, e)
	}
}

func TestMailFrequency194RejectsGenericBypassAmbiguousBodyQueryAndStaleAuthority(t *testing.T) {
	f := channel168Local(t)
	state := channel168Data(t, mailFrequency194Request(f, "GET", ""))
	body := `{"mail_interval_email":"60","mail_interval_ip":"60","mail_daily_email":"10","mail_daily_ip":"20","expected_fingerprint":"` + state["fingerprint"].(string) + `"}`
	for _, raw := range []string{strings.Replace(body, `"60"`, `0`, 1), strings.Replace(body, `"60"`, `"0"`, 1), strings.Replace(body, `"60"`, `"060"`, 1), strings.Replace(body, `"60"`, `"86401"`, 1), strings.Replace(body, `"mail_daily_ip":"20"`, `"mail_daily_ip":"100001"`, 1), strings.Replace(body, `"mail_daily_ip":"20"`, `"mail_daily_ip":"20","mail_daily_ip":"30"`, 1), strings.Replace(body, `"mail_daily_ip":"20"`, `"mail_daily_ip":"20","smtp_password":"synthetic-only"`, 1)} {
		if w := mailFrequency194Request(f, "PATCH", raw); w.Code != 400 && w.Code != 422 {
			t.Fatal("ambiguous/out-of-range frequency accepted", w.Code, raw)
		}
	}
	for _, target := range []string{"/console/v1/admin/settings/mail-frequency?", "/console/v1/admin/settings/mail-frequency?x=1"} {
		r := httptest.NewRequest("GET", target, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("query accepted", target, w.Code)
		}
	}
	for _, key := range []string{"mail_interval_email", "mail_interval_ip", "mail_daily_email", "mail_daily_ip"} {
		r := httptest.NewRequest("POST", "/console/v1/admin/resources/settings", strings.NewReader(`{"key":"`+key+`","value":"1"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code < 400 {
			t.Fatal("generic settings bypassed protected frequency", key, w.Code)
		}
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE console_users SET admin_class='operator' WHERE username='admin168'`); e != nil {
		t.Fatal(e)
	}
	// Persistent session resolution invalidates a token whose administrator
	// class no longer matches its durable account, before endpoint dispatch.
	if w := mailFrequency194Request(f, "PATCH", body); w.Code != 401 {
		t.Fatal("stale original super authority was not invalidated", w.Code)
	}
	var e error
	var version int64
	if e = f.p.QueryRow(t.Context(), `SELECT auth_version FROM console_users WHERE username='admin168'`).Scan(&version); e != nil {
		t.Fatal(e)
	}
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: 168, Username: "admin168", Role: "admin", AdminClass: "operator", AuthVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	if w := mailFrequency194Request(f, "PATCH", body); w.Code != 403 {
		t.Fatal("current operator bypassed super-only configuration", w.Code)
	}
}

func TestMailFrequency194IndependentWitnessPreventsOldValidPolicyAndAuditRollback(t *testing.T) {
	f := channel168Local(t)
	state := channel168Data(t, mailFrequency194Request(f, "GET", ""))
	for _, daily := range []string{"10", "9"} {
		body, _ := json.Marshal(map[string]any{"mail_interval_email": "60", "mail_interval_ip": "60", "mail_daily_email": daily, "mail_daily_ip": "20", "expected_fingerprint": state["fingerprint"]})
		state = channel168Data(t, mailFrequency194Request(f, "PATCH", string(body)))
	}
	// Privileged synthetic corruption keeps an old, individually valid policy
	// and its audit. The independent second witness must still reveal rollback.
	if _, e := f.p.Exec(t.Context(), `ALTER TABLE mail_rate_policies194 DISABLE TRIGGER ALL;DELETE FROM mail_rate_policies194 WHERE revision=2;ALTER TABLE mail_rate_policies194 ENABLE TRIGGER ALL;ALTER TABLE backoffice_audit DISABLE TRIGGER USER;DELETE FROM backoffice_audit WHERE resource='mail-frequency194' AND resource_id=2;ALTER TABLE backoffice_audit ENABLE TRIGGER USER;UPDATE mail_rate_head194 SET revision=1`); e != nil {
		t.Fatal(e)
	}
	if w := mailFrequency194Request(f, "GET", ""); w.Code != 503 {
		t.Fatal("old valid policy/audit rollback accepted", w.Code, w.Body.String())
	}
}
