package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/identity"
)

type scheduler171Fixture struct {
	s           *Server
	token, csrf string
}

func scheduler171Local(t *testing.T) scheduler171Fixture {
	t.Helper()
	s, p := groupRoutingFixture152(t)
	s.Sessions = identity.NewPersistentSessions(p, false)
	if _, e := p.Exec(context.Background(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('scheduler171','admin',171,'Synthetic Scheduler','unused-synthetic-hash','super'),('scheduler171-backup','admin',173,'Synthetic Backup','unused-synthetic-hash','super')`); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{ID: 171, Role: "admin", Username: "scheduler171", AdminClass: "super", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	return scheduler171Fixture{s, token, csrf}
}
func (f scheduler171Fixture) request(method, path, body, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/scheduled-tasks"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func scheduler171Data(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("scheduler HTTP=%d want200", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("scheduler response cached")
	}
	var e struct {
		Code int
		Data map[string]any
	}
	if json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Code != 0 || e.Data == nil {
		t.Fatal("invalid scheduler envelope")
	}
	for _, secret := range []string{"lease_token", "proof_hash", "source_hash", "password", "api_key", "actor_name"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("private scheduler fact leaked", secret)
		}
	}
	return e.Data
}
func TestScheduler171RegistryDefaultsAndOldMetadataNeverExecute(t *testing.T) {
	f := scheduler171Local(t)
	if _, e := f.s.Pool.Exec(context.Background(), `INSERT INTO backoffice_records(resource,body)VALUES('cron-tasks','{"name":"legacy enabled","kind":"payment.notify","enabled":true,"interval_seconds":60}')`); e != nil {
		t.Fatal(e)
	}
	catalog := scheduler171Data(t, f.request("GET", "", "", f.csrf))
	items, ok := catalog["items"].([]any)
	if !ok || len(items) < 2 {
		t.Fatal("no trusted task registry")
	}
	found := false
	for _, item := range items {
		v := item.(map[string]any)
		if v["task"] == "referral_daily" {
			found = true
			if v["enabled"] != false || v["configured"] != false || v["revision"] != "0" || v["last_run_time"] != nil || v["next_run_time"] != nil || v["last_status"] != "never_run" {
				t.Fatal("new task fabricated activity", v)
			}
		}
	}
	if !found {
		t.Fatal("real local referral executor missing")
	}
	w := f.request("POST", "/referral_daily/run", `{"expected_fingerprint":"`+strings.Repeat("0", 64)+`"}`, f.csrf)
	if w.Code != 409 {
		t.Fatal("disabled task could be scheduled", w.Code)
	}
	var runs int
	if e := f.s.Pool.QueryRow(context.Background(), `SELECT count(*) FROM scheduled_task_runs`).Scan(&runs); e != nil || runs != 0 {
		t.Fatal("GET/legacy enabled metadata created execution", runs, e)
	}
}
func TestScheduler171StrictCASAndManualRequestIsPendingOnly(t *testing.T) {
	f := scheduler171Local(t)
	before := scheduler171Data(t, f.request("GET", "/referral_daily", "", f.csrf))
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Second).Format(time.RFC3339)
	body := fmt.Sprintf(`{"enabled":true,"schedule":{"frequency":"once","interval":1,"execute_at":%q},"params":{"limit":20},"expected_fingerprint":%q}`, at, before["fingerprint"])
	if w := f.request("PATCH", "/referral_daily", body, ""); w.Code != 403 {
		t.Fatal("missing CSRF changed schedule", w.Code)
	}
	after := scheduler171Data(t, f.request("PATCH", "/referral_daily", body, f.csrf))
	if after["revision"] != "1" || after["enabled"] != true {
		t.Fatal("saved policy was not exact", after)
	}
	if w := f.request("PATCH", "/referral_daily", body, f.csrf); w.Code != 409 {
		t.Fatal("stale CAS overwrote task", w.Code)
	}
	trigger := fmt.Sprintf(`{"expected_fingerprint":%q}`, after["fingerprint"])
	run := scheduler171Data(t, f.request("POST", "/referral_daily/run", trigger, f.csrf))
	if run["status"] != "pending" || run["completed_at"] != nil {
		t.Fatal("manual scheduling falsely reported completion", run)
	}
	if w := f.request("POST", "/referral_daily/run", trigger, f.csrf); w.Code != 409 {
		t.Fatal("same reviewed trigger duplicated a run", w.Code)
	}
	var pending, results, journals int
	if e := f.s.Pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM scheduled_task_runs),(SELECT count(*) FROM scheduled_task_results),(SELECT count(*) FROM journals)`).Scan(&pending, &results, &journals); e != nil || pending != 1 || results != 0 || journals != 0 {
		t.Fatal("HTTP scheduling executed business or duplicated intent", pending, results, journals, e)
	}
	history := scheduler171Data(t, f.request("GET", "/referral_daily/runs?limit=1&before_id=0", "", f.csrf))
	if len(history["items"].([]any)) != 1 {
		t.Fatal("pending run missing from durable history")
	}
	for _, invalid := range []string{`{"enabled":true,"enabled":false}`, `null`, `{"unexpected":1}`} {
		if w := f.request("PATCH", "/referral_daily", invalid, f.csrf); w.Code != 400 {
			t.Fatal("loose task JSON accepted", w.Code)
		}
	}
	if w := f.request("GET", "?merchant_id=1", "", f.csrf); w.Code != 400 {
		t.Fatal("catalog accepted tenant selector", w.Code)
	}
	// 'complain' is now an AVAILABLE scheduled task (ux-complain enabled it in the
	// registry; see payment.TestComplainScheduledTaskIsAvailableAndSchedulable), so
	// the unavailable-external-handler guard is exercised against 'applyrefresh',
	// which remains unavailable ("相关外部业务插件暂缓。"). Its own parameter
	// (refresh_minutes) is supplied so the request passes shape validation and the
	// 422 comes from the unavailable-executor guard, not a malformed body.
	applyrefresh := strings.Replace(body, `{"limit":20}`, `{"refresh_minutes":60}`, 1)
	if w := f.request("PATCH", "/applyrefresh", applyrefresh, f.csrf); w.Code != 422 {
		t.Fatal("unavailable external handler accepted config", w.Code)
	}
}

func TestScheduler171DeferredOriginalParamsAreReadOnlyAndNotFakeExecutors(t *testing.T) {
	f := scheduler171Local(t)
	value := scheduler171Data(t, f.request("GET", "/applyrefresh", "", f.csrf))
	definitions := value["parameter_definitions"].([]any)
	if value["available"] != false || value["can_update"] != false || value["can_run"] != false || len(definitions) != 1 {
		t.Fatal("deferred task lost original parameter contract or pretended executable")
	}
	parameter := definitions[0].(map[string]any)
	if parameter["name"] != "refresh_minutes" || parameter["min"] != float64(15) || parameter["max"] != nil || parameter["default"] != float64(60) {
		t.Fatal("original Applyrefresh parameter units/default changed")
	}
	params := value["params"].(map[string]any)
	if params["refresh_minutes"] != float64(60) || len(params) != 1 {
		t.Fatal("unavailable task was given fake local limit parameters")
	}
	if w := f.request("POST", "/applyrefresh/run", fmt.Sprintf(`{"expected_fingerprint":%q}`, value["fingerprint"]), f.csrf); w.Code != 422 {
		t.Fatal("unavailable task accepted a run", w.Code)
	}
}
