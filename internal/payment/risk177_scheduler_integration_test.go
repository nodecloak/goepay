package payment

import (
	"context"
	"errors"
	"goepay/internal/riskpolicy"
	"testing"
	"time"
)

func risk177SchedulerFixture(t *testing.T) (*Store, scheduledTaskRun) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	p, e := s.CoreRiskPolicy(ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	settings := riskpolicy.Defaults()
	settings["auto_check_payip"] = "1"
	settings["check_payip_second"] = "3600"
	settings["check_payip_count"] = "1"
	if _, e = s.UpdateCoreRiskPolicy(ctx, a, settings, p.Fingerprint); e != nil {
		t.Fatal(e)
	}
	if e = s.CreateMerchant(ctx, Merchant{ID: 1771, Name: "Synthetic check", APIKey: "test-only"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1771, OutTradeNo: "risk177-scheduled-unpaid", Name: "synthetic", Type: "alipay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetOrderClientIP(ctx, o.ID, "192.0.2.177"); e != nil {
		t.Fatal(e)
	}
	views, e := s.ScheduledTasks(ctx, a, "check")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UpdateScheduledTask(ctx, a, "check", ScheduledTaskInput{Enabled: true, Schedule: ScheduledTaskSchedule{"once", 1, time.Now().UTC().Add(time.Hour).Truncate(time.Second)}, Params: ScheduledTaskParams{}, ExpectedFingerprint: views[0].Fingerprint}); e != nil {
		t.Fatal(e)
	}
	scheduler171Pending(t, s, a, "check")
	conn, e := s.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	r, claimed, e := s.claimScheduledRun(ctx, conn, "check", time.Minute)
	if e != nil || !claimed {
		t.Fatal("real original Check not claimable", claimed, e)
	}
	return s, r
}
func TestRisk177ScheduledCheckRealExecutionAndReplay(t *testing.T) {
	s, r := risk177SchedulerFixture(t)
	ctx := context.Background()
	first, e := s.executeScheduledTask(ctx, r)
	if e != nil || first.Examined != 1 || first.Generated != 1 {
		t.Fatal("real Check did not create risk action", first, e)
	}
	second, e := s.executeScheduledTask(ctx, r)
	if e != nil || second != first {
		t.Fatal("same original run did not recover existing Check", second, e)
	}
	var actions, runs int
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM core_risk_actions),(SELECT count(*)FROM core_risk_check_runs)`).Scan(&actions, &runs); e != nil || actions != 1 || runs != 1 {
		t.Fatal(actions, runs, e)
	}
	if e = s.completeScheduledLease(ctx, r, first, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = s.executeScheduledTask(ctx, r); !errors.Is(e, ErrConflict) {
		t.Fatal("completed original lease executed Check", e)
	}
}
func TestRisk177ScheduledCheckFinalAuditLeaseExpiryRollsBackAllEffects(t *testing.T) {
	s, r := risk177SchedulerFixture(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `UPDATE scheduled_task_runs SET lease_until=clock_timestamp()+interval '300 milliseconds' WHERE id=$1`, r.id); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `CREATE SEQUENCE risk177_check_last_write;CREATE FUNCTION risk177_check_delay()RETURNS TRIGGER LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='core-risk-check' THEN PERFORM nextval('risk177_check_last_write');PERFORM pg_sleep(0.6);END IF;RETURN NEW;END$$;CREATE TRIGGER risk177_check_delay BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION risk177_check_delay()`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.executeScheduledTask(ctx, r); !errors.Is(e, ErrConflict) {
		t.Fatal("expired Check original lease committed risk effects", e)
	}
	var actions, runs, audits, notices int
	var entered bool
	if e := s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM core_risk_actions),(SELECT count(*)FROM core_risk_check_runs),(SELECT count(*)FROM backoffice_audit WHERE resource IN('core-risk-check','core-risk-actions')),(SELECT count(*)FROM core_risk_notices),(SELECT is_called FROM risk177_check_last_write)`).Scan(&actions, &runs, &audits, &notices, &entered); e != nil {
		t.Fatal(e)
	}
	if !entered || actions != 0 || runs != 0 || audits != 0 || notices != 0 {
		t.Fatal("last audit rollback incomplete", entered, actions, runs, audits, notices)
	}
}
