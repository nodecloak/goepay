package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScheduler171ActualOperationAndRuntimeLogsNeverExposeRecoveryFacts(t *testing.T) {
	f := scheduler171Local(t)
	ctx := context.Background()
	state := scheduler171Data(t, f.request("GET", "/referral_daily", "", f.csrf))
	saved := scheduler171Data(t, f.request("PATCH", "/referral_daily", scheduler171Body(state["fingerprint"].(string)), f.csrf))
	scheduler171Data(t, f.request("POST", "/referral_daily/run", `{"expected_fingerprint":"`+saved["fingerprint"].(string)+`"}`, f.csrf))
	if tick, e := f.s.Store.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	var intent, progress, leaseHash string
	if e := f.s.Pool.QueryRow(ctx, `SELECT encode(source_hash,'hex'),encode(progress_hash,'hex'),(SELECT changes->>'token_hash' FROM backoffice_audit WHERE resource='scheduled-task-execution' AND operation='lease' ORDER BY id DESC LIMIT 1)FROM scheduled_task_runs ORDER BY id DESC LIMIT 1`).Scan(&intent, &progress, &leaseHash); e != nil || len(leaseHash) != 64 {
		t.Fatal("non-secret lease commitment missing", e)
	}
	const private = "scheduler171-DO-NOT-EXPOSE-original-recovery-material"
	if _, e := f.s.Pool.Exec(ctx, `CREATE FUNCTION scheduler171_private_failure()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='scheduled-tasks' THEN RAISE EXCEPTION 'scheduler171-DO-NOT-EXPOSE-original-recovery-material';END IF;RETURN NEW;END$$;CREATE TRIGGER scheduler171_private_failure BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION scheduler171_private_failure()`); e != nil {
		t.Fatal(e)
	}
	state = scheduler171Data(t, f.request("GET", "/referral_daily", "", f.csrf))
	failed := f.request("PATCH", "/referral_daily", scheduler171Body(state["fingerprint"].(string)), f.csrf)
	if failed.Code != 503 || strings.Contains(failed.Body.String(), private) {
		t.Fatal("raw internal fault returned", failed.Code)
	}
	for _, name := range []string{"audit-log", "system-log", "runtime-log", "jobs"} {
		r := httptest.NewRequest("GET", "/console/v1/admin/operations/"+name+"?limit=100", nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal("actual log GET", name, w.Code)
		}
		for _, value := range []string{private, intent, progress, leaseHash, "lease_token", "token_hash", "intent_hash", "progress_hash", "MailCipher", "scheduled_notification_mail_head", "source_hash", "authority_token"} {
			if value != "" && strings.Contains(w.Body.String(), value) {
				t.Fatal("private recovery field leaked via original log projection", name, value)
			}
		}
		if name == "runtime-log" && !strings.Contains(w.Body.String(), "P0001") {
			t.Fatal("actual rejected scheduler transaction did not reach bounded original runtime event")
		}
	}
}
