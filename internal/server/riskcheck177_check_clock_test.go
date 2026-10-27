package server

import (
	"context"
	"errors"
	"goepay/internal/payment"
	"testing"
	"time"
)

func TestRisk177CheckFinalAuditWaitCannotPunishExpiredObservationWindow(t *testing.T) {
	f, _, _ := risk177Flow(t)
	ctx := context.Background()
	v := risk177Data(t, f.request("GET", "", f.csrf))
	risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
		s["auto_check_payip"] = "1"
		s["check_payip_second"] = "2"
		s["check_payip_count"] = "1"
	}), f.csrf))
	p := f.pay(t, "risk177-observation-final-wait", "192.0.2.177")
	if p["code"] != float64(1) {
		t.Fatal(p)
	}
	// Preserve the new104 birth MAC and let the original observation clock
	// elapse. Editing created_at now correctly invalidates its immutable source.
	time.Sleep(1500 * time.Millisecond)
	if _, e := f.s.Pool.Exec(ctx, `CREATE SEQUENCE risk177_check_window;CREATE FUNCTION risk177_check_window_delay()RETURNS TRIGGER LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='core-risk-check' THEN PERFORM nextval('risk177_check_window');PERFORM pg_sleep(0.8);END IF;RETURN NEW;END$$;CREATE TRIGGER risk177_check_window_delay BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION risk177_check_window_delay()`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.Store.RunCoreRiskCheck(ctx, "risk177-final-observation"); !errors.Is(e, payment.ErrState) {
		t.Fatal("expired observation committed risk action", e)
	}
	var actions, runs int
	var entered bool
	if e := f.s.Pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM core_risk_actions),(SELECT count(*)FROM core_risk_check_runs),(SELECT is_called FROM risk177_check_window)`).Scan(&actions, &runs, &entered); e != nil {
		t.Fatal(e)
	}
	if !entered || actions != 0 || runs != 0 {
		t.Fatal("not a real final-write rollback", entered, actions, runs)
	}
}
