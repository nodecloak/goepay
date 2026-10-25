package payment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"goepay/internal/platform/jobs"
)

func settlementRecoveryReady(t *testing.T, lifetime ...time.Duration) (*Store, SettlementPlanView, SettlementPlanAuthorizationView, SettlementPlanRunView, context.Context) {
	t.Helper()
	s := testStore(t)
	p := settlementPlanReady(t, s, 901)
	// A once plan cannot simply wait for the next periodic slot after an outage.
	input := settlementPlanInputFor(901)
	var now time.Time
	if err := s.pool.QueryRow(context.Background(), "SELECT clock_timestamp()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	input.Schedule = SettlementScheduleSpec{Frequency: "once", AnchorAt: now.Add(-time.Minute).Truncate(time.Second)}
	p, err := s.UpdateSettlementPlan(context.Background(), p.ID, p.Revision, input, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	guarded := settlementAuthorityReviewGuard(t, s)
	authorization := settlementPlanAuthorizationInput(t, s)
	if len(lifetime) == 1 {
		authorization.ExpiresAt = authorization.ValidFrom.Add(time.Minute + lifetime[0])
	}
	a, err := s.AuthorizeSettlementPlan(guarded, p.ID, p.Revision, authorization, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateSettlementPlanRun(context.Background(), p.ID, p.Revision, settlementPlanAdmin)
	if err != nil {
		t.Fatal(err)
	}
	return s, p, a, run, guarded
}

// Use the real claim/retry path to exhaust the job, rather than manufacturing a
// dead row that the queue could never produce. No provider or payment I/O occurs.
func settlementRecoveryExhaust(t *testing.T, s *Store, runID int64) jobs.Job {
	t.Helper()
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, "UPDATE jobs SET max_attempts=1 WHERE event_key=$1", fmt.Sprintf("settlement-plan-run:%d", runID)); err != nil {
		t.Fatal(err)
	}
	q := jobs.Queue{Pool: s.pool}
	j, err := q.Claim(ctx, []string{"settlement-plan.generate"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = q.Retry(ctx, j, "local transient dependency unavailable", 0); err != nil {
		t.Fatal(err)
	}
	return j
}

func settlementRecoveryJobJSON(t *testing.T, s *Store, id int64) string {
	t.Helper()
	var out string
	if err := s.pool.QueryRow(context.Background(), "SELECT to_jsonb(j)::text FROM jobs j WHERE id=$1", id).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSettlementPlanRecoveryManualRestoresOriginalDeadJobAndOneReserve(t *testing.T) {
	s, p, _, run, _ := settlementRecoveryReady(t)
	ctx := context.Background()
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 901, OutTradeNo: "local-recovery-income", Name: "Local", Amount: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkPaid(ctx, o.TradeNo, "local-recovery-receipt"); err != nil {
		t.Fatal(err)
	}
	j := settlementRecoveryExhaust(t, s, run.ID)
	dead := settlementRecoveryJobJSON(t, s, j.ID)
	if err = s.ScheduleSettlementPlans(ctx); err != nil {
		t.Fatal(err)
	}
	if got := settlementRecoveryJobJSON(t, s, j.ID); got != dead {
		t.Fatal("automatic tick reset exhausted retry budget", got)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, e := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin)
			if e != nil || out.ID != run.ID || out.AuthorizationID != run.AuthorizationID {
				t.Errorf("manual recovery did not preserve original run: %+v %v", out, e)
			}
		}()
	}
	wg.Wait()
	var state, key, payload, lastError string
	var attempts, jobCount, audits int
	var cleanLease bool
	if err = s.pool.QueryRow(ctx, `SELECT state,attempts,event_key,payload::text,last_error,lease_token IS NULL AND lease_until IS NULL,
 (SELECT count(*) FROM jobs WHERE kind='settlement-plan.generate'),
 (SELECT count(*) FROM backoffice_audit WHERE resource='settlement-plan-runs' AND resource_id=$2 AND operation='recover' AND actor_role='admin' AND actor_id=1 AND actor_name='local-plan-admin') FROM jobs WHERE id=$1`, j.ID, run.ID).Scan(&state, &attempts, &key, &payload, &lastError, &cleanLease, &jobCount, &audits); err != nil || state != "pending" || attempts != 0 || lastError != "" || !cleanLease || jobCount != 1 || audits != 1 || key != fmt.Sprintf("settlement-plan-run:%d", run.ID) || payload != fmt.Sprintf(`{"run_id": "%d"}`, run.ID) {
		t.Fatalf("dead original job was not recovered exactly once with audit: state=%s attempts=%d jobs=%d audits=%d key=%s payload=%s err=%v", state, attempts, jobCount, audits, key, payload, err)
	}
	q := jobs.Queue{Pool: s.pool}
	claimed, err := q.Claim(ctx, []string{"settlement-plan.generate"}, time.Minute)
	if err != nil || claimed.ID != j.ID {
		t.Fatal("recovered original job cannot be claimed", claimed, err)
	}
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if err = q.Complete(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	var requests, intents, uses int
	if err = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM settlement_scheduled_intents),(SELECT count(*) FROM settlement_plan_budget_uses)`).Scan(&requests, &intents, &uses); err != nil || requests != 1 || intents != 1 || uses != 1 {
		t.Fatal("recovery duplicated original reserve", requests, intents, uses, err)
	}
	if balance, e := s.Balance(ctx, 901); e != nil || balance.Available != 1000 || balance.Reserved != 1000 {
		t.Fatal("wrong recovered balance", balance, e)
	}
	done := settlementRecoveryJobJSON(t, s, j.ID)
	if _, err = s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	if got := settlementRecoveryJobJSON(t, s, j.ID); got != done {
		t.Fatal("completed original job was rearmed", got)
	}
}

func TestSettlementPlanRecoveryRejectsReplacedAuthority(t *testing.T) {
	s, p, a, run, guarded := settlementRecoveryReady(t)
	j := settlementRecoveryExhaust(t, s, run.ID)
	in := settlementPlanAuthorizationInput(t, s)
	in.ExpectedAuthorizationID = a.ID
	in.MaxItemGross, in.MaxRunGross, in.MaxDayGross = 100, 100, 100
	if _, err := s.AuthorizeSettlementPlan(guarded, p.ID, p.Revision, in, settlementPlanAdmin); err != nil {
		t.Fatal(err)
	}
	before := settlementRecoveryJobJSON(t, s, j.ID)
	if _, err := s.CreateSettlementPlanRun(context.Background(), p.ID, p.Revision, settlementPlanAdmin); !errors.Is(err, ErrConflict) {
		t.Fatal("new smaller authorization rearmed old run authority", err)
	}
	if after := settlementRecoveryJobJSON(t, s, j.ID); after != before {
		t.Fatal("replaced original authority modified job", after)
	}
}

func TestSettlementPlanRecoveryAuditFailureRollsBackJob(t *testing.T) {
	s, p, _, run, _ := settlementRecoveryReady(t)
	j := settlementRecoveryExhaust(t, s, run.ID)
	ctx := context.Background()
	before := settlementRecoveryJobJSON(t, s, j.ID)
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION local_recovery_audit_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='settlement-plan-runs' AND NEW.operation='recover' THEN RAISE EXCEPTION 'local recovery audit failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER local_recovery_audit_fail BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION local_recovery_audit_fail()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin); err == nil {
		t.Fatal("recovery ignored audit insertion failure")
	}
	if got := settlementRecoveryJobJSON(t, s, j.ID); got != before {
		t.Fatal("failed audit committed recovered job", got)
	}
	if _, err := s.pool.Exec(ctx, "DROP TRIGGER local_recovery_audit_fail ON backoffice_audit"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin); err != nil {
		t.Fatal("original job could not recover after audit dependency restored", err)
	}
}

func TestSettlementPlanRecoveryDoesNotResetActiveOrCompletedJobs(t *testing.T) {
	for _, state := range []string{"pending", "running", "succeeded"} {
		t.Run(state, func(t *testing.T) {
			s, p, _, run, _ := settlementRecoveryReady(t)
			ctx := context.Background()
			var jobID int64
			if err := s.pool.QueryRow(ctx, "SELECT id FROM jobs WHERE event_key=$1", fmt.Sprintf("settlement-plan-run:%d", run.ID)).Scan(&jobID); err != nil {
				t.Fatal(err)
			}
			if state != "pending" {
				q := jobs.Queue{Pool: s.pool}
				claimed, err := q.Claim(ctx, []string{"settlement-plan.generate"}, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				if state == "succeeded" {
					if err = q.Complete(ctx, claimed); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := settlementRecoveryJobJSON(t, s, jobID)
			if out, err := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin); err != nil || out.ID != run.ID {
				t.Fatal("original scan replay failed", out, err)
			}
			if got := settlementRecoveryJobJSON(t, s, jobID); got != before {
				t.Fatal("manual scan changed a non-dead job", state, got)
			}
		})
	}
}

func TestSettlementPlanRecoveryRejectsMismatchedOriginalJob(t *testing.T) {
	for _, alteration := range []string{
		`kind='payment.notify'`,
		`payload='{"run_id":"999999"}'`,
		`payload='{"run_id":"1","extra":"ignored"}'`,
	} {
		t.Run(alteration, func(t *testing.T) {
			s, p, _, run, _ := settlementRecoveryReady(t)
			j := settlementRecoveryExhaust(t, s, run.ID)
			if _, err := s.pool.Exec(context.Background(), "UPDATE jobs SET "+alteration+" WHERE id=$1", j.ID); err != nil {
				t.Fatal(err)
			}
			before := settlementRecoveryJobJSON(t, s, j.ID)
			if _, err := s.CreateSettlementPlanRun(context.Background(), p.ID, p.Revision, settlementPlanAdmin); !errors.Is(err, ErrConflict) {
				t.Fatal("unrelated job identity was accepted as original recovery", err)
			}
			if got := settlementRecoveryJobJSON(t, s, j.ID); got != before {
				t.Fatal("mismatched original job was rewritten", got)
			}
		})
	}
}

func TestSettlementPlanRecoveryRejectsRevocationAndChangedDefinition(t *testing.T) {
	for _, reason := range []string{"revoked", "definition_replaced"} {
		t.Run(reason, func(t *testing.T) {
			s, p, a, run, guarded := settlementRecoveryReady(t)
			ctx := context.Background()
			j := settlementRecoveryExhaust(t, s, run.ID)
			if reason == "revoked" {
				if err := s.RevokeSettlementPlanAuthorization(ctx, a.ID, settlementPlanAdmin); err != nil {
					t.Fatal(err)
				}
			} else {
				in := settlementPlanInputFor(901)
				view, err := s.SettlementPlan(ctx, p.ID, settlementPlanAdmin)
				if err != nil {
					t.Fatal(err)
				}
				in.Schedule = view.Schedule
				in.Name = "replacement definition"
				p, err = s.UpdateSettlementPlan(ctx, p.ID, p.Revision, in, settlementPlanAdmin)
				if err != nil {
					t.Fatal(err)
				}
				authority := settlementPlanAuthorizationInput(t, s)
				authority.ExpectedAuthorizationID = a.ID
				if _, err = s.AuthorizeSettlementPlan(guarded, p.ID, p.Revision, authority, settlementPlanAdmin); err != nil {
					t.Fatal(err)
				}
			}
			before := settlementRecoveryJobJSON(t, s, j.ID)
			if _, err := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin); !errors.Is(err, ErrState) && !errors.Is(err, ErrConflict) {
				t.Fatal("old revoked/replaced run was rearmed", reason, err)
			}
			if got := settlementRecoveryJobJSON(t, s, j.ID); got != before {
				t.Fatal("ineligible original job changed", got)
			}
		})
	}
}

func TestSettlementPlanRecoveryWaitsForOriginalJobAndRechecksExpiry(t *testing.T) {
	s, p, a, run, _ := settlementRecoveryReady(t, 2*time.Second)
	ctx := context.Background()
	j := settlementRecoveryExhaust(t, s, run.ID)
	before := settlementRecoveryJobJSON(t, s, j.ID)
	lock, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(ctx)
	if _, err = lock.Exec(ctx, "SELECT id FROM jobs WHERE id=$1 FOR UPDATE", j.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, e := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin); done <- e }()
	select {
	case e := <-done:
		t.Fatal("recovery did not wait for original job lock", e)
	case <-time.After(150 * time.Millisecond):
	}
	// The host and Docker PostgreSQL clocks need not be identical. Observe the
	// database deadline itself before releasing the job lock.
	wait, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	for {
		var databaseNow time.Time
		if err = s.pool.QueryRow(wait, "SELECT clock_timestamp()").Scan(&databaseNow); err != nil {
			t.Fatal("database authority deadline did not arrive", err)
		}
		if !databaseNow.Before(a.ExpiresAt) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err = lock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-done:
		if !errors.Is(e, ErrState) {
			t.Fatal("authority expired during job lock wait but recovery succeeded", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("recovery did not finish after lock released")
	}
	if got := settlementRecoveryJobJSON(t, s, j.ID); got != before {
		t.Fatal("expired original job was rearmed", got)
	}
}

func TestSettlementPlanRecoveryRequiresExplicitSuperActor(t *testing.T) {
	s, p, _, run, _ := settlementRecoveryReady(t)
	j := settlementRecoveryExhaust(t, s, run.ID)
	before := settlementRecoveryJobJSON(t, s, j.ID)
	for _, actor := range []SettlementPlanActor{
		{Role: "admin", Class: "operator", ID: 1, Name: "local-operator"},
		{Role: "system", Class: "super", ID: 1, Name: "local-system"},
		{Role: "merchant", Class: "super", ID: 901, Name: "local-merchant"},
		{Role: "admin", Class: "super", ID: 0, Name: "local-missing-actor"},
	} {
		if _, err := s.CreateSettlementPlanRun(context.Background(), p.ID, p.Revision, actor); !errors.Is(err, ErrInvalid) {
			t.Fatal("non-super actor recovered original job", actor, err)
		}
	}
	if got := settlementRecoveryJobJSON(t, s, j.ID); got != before {
		t.Fatal("denied actor modified original job", got)
	}
}
