package server

import (
	"context"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"testing"
	"time"
)

func TestRisk177OriginalScheduledChildCheckLastAuditLeaseRollsBackNoticeAndAction(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	risk := risk177Fixture{f.s, f.token, f.csrf}
	ctx := context.Background()
	if w := f.childHTTP("POST", "", `{"merchant_id":"1","parent_id":"7","name":"Synthetic scheduled child","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"risk177-scheduled-child","secret":"synthetic-only-secret"}}`); w.Code != 200 {
		t.Fatal("create", w.Code)
	}
	v := risk177Data(t, risk.request("GET", "", risk.csrf))
	risk177Data(t, risk.request("PATCH", risk177Body(t, v, func(s map[string]any) {
		s["auto_check_channel"] = "1"
		s["check_channel_second"] = "3600"
		s["check_channel_failcount"] = "1"
		s["check_channel_notice"] = "1"
	}), risk.csrf))
	if out := f.pay(t, "risk177-scheduled-child-first", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	a := payment.MerchantCredentialActor{User: identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1}, SessionToken: f.token}
	states, e := f.s.Store.ScheduledTasks(ctx, a, "check")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Store.UpdateScheduledTask(ctx, a, "check", payment.ScheduledTaskInput{Enabled: true, Schedule: payment.ScheduledTaskSchedule{Frequency: "once", Interval: 1, ExecuteAt: time.Now().UTC().Add(-time.Second).Truncate(time.Second)}, Params: payment.ScheduledTaskParams{}, ExpectedFingerprint: states[0].Fingerprint}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(ctx, `CREATE SEQUENCE risk177_child_lease_last;CREATE FUNCTION risk177_child_lease_last()RETURNS TRIGGER LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='core-risk-check' THEN PERFORM nextval('risk177_child_lease_last');UPDATE scheduled_task_runs SET lease_until=clock_timestamp()-interval '1 second'WHERE task_key='check'AND state='running';END IF;RETURN NEW;END$$;CREATE TRIGGER risk177_child_lease_last BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION risk177_child_lease_last()`); e != nil {
		t.Fatal(e)
	}
	tick, e := f.s.Store.RunScheduledTasksOnce(ctx, 1)
	if e != nil || tick.Claimed != 1 || tick.Failed != 1 || tick.Completed != 0 {
		t.Fatal("real Check scheduler did not fail original expired transaction", tick, e)
	}
	var actions, notices, jobs, runs, generated int
	var entered bool
	if e = f.p.QueryRow(ctx, `SELECT(SELECT count(*)FROM core_risk_actions),(SELECT count(*)FROM core_risk_notices),(SELECT count(*)FROM jobs WHERE kind='core-risk.notice'),(SELECT count(*)FROM core_risk_check_runs),(SELECT COALESCE((counts->>'generated')::int,0)FROM scheduled_task_results LIMIT 1),(SELECT is_called FROM risk177_child_lease_last)`).Scan(&actions, &notices, &jobs, &runs, &generated, &entered); e != nil {
		t.Fatal(e)
	}
	if !entered || actions != 0 || notices != 0 || jobs != 0 || runs != 0 || generated != 0 || factory.providers["risk177-scheduled-child"].sends.Load() != 1 {
		t.Fatal("expired original Check leaked risk effects or false success counts", entered, actions, notices, jobs, runs, generated)
	}
	if _, e = f.p.Exec(ctx, `DROP TRIGGER risk177_child_lease_last ON backoffice_audit;DROP FUNCTION risk177_child_lease_last()`); e != nil {
		t.Fatal(e)
	}
	states, e = f.s.Store.ScheduledTasks(ctx, a, "check")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Store.RequestScheduledTaskRun(ctx, a, "check", states[0].Fingerprint); e != nil {
		t.Fatal(e)
	}
	tick, e = f.s.Store.RunScheduledTasksOnce(ctx, 1)
	if e != nil || tick.Completed != 1 || tick.Failed != 0 {
		t.Fatal("new explicit original Check could not complete", tick, e)
	}
	if e = f.p.QueryRow(ctx, `SELECT(SELECT count(*)FROM core_risk_actions),(SELECT count(*)FROM core_risk_notices),(SELECT count(*)FROM jobs WHERE kind='core-risk.notice')`).Scan(&actions, &notices, &jobs); e != nil || actions != 1 || notices != 1 || jobs != 1 {
		t.Fatal("successful original child Check did not capture real notice", actions, notices, jobs, e)
	}
}
