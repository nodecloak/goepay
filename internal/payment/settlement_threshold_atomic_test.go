package payment

import (
	"context"
	"sync"
	"testing"
)

func thresholdFundedSingle74(t *testing.T, s *Store, mid int64) SettlementPlanRunView {
	t.Helper()
	ctx := context.Background()
	return thresholdPreparedRun74(t, s, mid, func() {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "atomic-threshold-income", Amount: 1000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.MarkPaid(ctx, o.TradeNo, "atomic-threshold-local"); e != nil {
			t.Fatal(e)
		}
	})
}

func TestSettlementThreshold74ConcurrentOriginalRunReservesOneWholeWallet(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run := thresholdFundedSingle74(t, s, 7428)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.GenerateSettlementPlanRun(ctx, run.ID) }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var requests, items, intents, sources int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM settlement_plan_items),(SELECT count(*) FROM settlement_threshold_intents),(SELECT count(*) FROM settlement_threshold_selections)").Scan(&requests, &items, &intents, &sources); e != nil || requests != 1 || items != 1 || intents != 1 || sources != 1 {
		t.Fatal(requests, items, intents, sources, e)
	}
}

func TestSettlementThreshold74AuditFailureRollsBackSelectionReserveAndAllFinancialIntent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run := thresholdFundedSingle74(t, s, 7429)
	var before int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM journals").Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION threshold_audit_failure74() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='settlement-plan-items' AND NEW.operation='generate' THEN RAISE EXCEPTION 'local threshold audit fault';END IF;RETURN NEW;END $$;CREATE TRIGGER threshold_audit_failure74 BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION threshold_audit_failure74()`); e != nil {
		t.Fatal(e)
	}
	if e := s.GenerateSettlementPlanRun(ctx, run.ID); e == nil {
		t.Fatal("audit fault was ignored")
	}
	var requests, items, sources, budget, journals, version int
	var available int64
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM settlement_plan_items),(SELECT count(*) FROM settlement_threshold_selections),(SELECT count(*) FROM settlement_plan_budget_uses),(SELECT count(*) FROM journals),(SELECT threshold_source_version FROM settlement_plan_runs WHERE id=$1),(SELECT balance FROM accounts WHERE owner_id=7429 AND kind='available' AND scope_id=0)", run.ID).Scan(&requests, &items, &sources, &budget, &journals, &version, &available); e != nil || requests != 0 || items != 0 || sources != 0 || budget != 0 || journals != before || version != 0 || available != 1000 {
		t.Fatal("partial durable funds after failed audit", requests, items, sources, budget, journals, version, available, e)
	}
	if _, e := s.pool.Exec(ctx, "DROP TRIGGER threshold_audit_failure74 ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if e := s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
		t.Fatal("rolled-back source prevented a valid original retry", e)
	}
}

func TestSettlementThreshold74FinalAuditWaitCannotCommitExpiredAuthority(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	first := thresholdFundedSingle74(t, s, 7430)
	if _, e := s.pool.Exec(ctx, `CREATE SEQUENCE threshold_final_audit_entered74;
 CREATE FUNCTION threshold_audit_wait74() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE deadline timestamptz; BEGIN
 IF NEW.resource='settlement-plan-items' AND NEW.operation='generate' THEN
 PERFORM nextval('threshold_final_audit_entered74');
 SELECT a.expires_at INTO deadline FROM settlement_plan_items i JOIN settlement_plan_runs r ON r.id=i.run_id JOIN settlement_plan_authorizations a ON a.id=r.authorization_id WHERE i.id=NEW.resource_id;
 IF deadline IS NULL THEN RAISE EXCEPTION 'synthetic final audit lost original authorization';END IF;
 PERFORM pg_sleep(GREATEST(0,extract(epoch FROM deadline-clock_timestamp())+0.05));END IF;RETURN NEW;END $$;
 CREATE TRIGGER threshold_audit_wait74 BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION threshold_audit_wait74()`); e != nil {
		t.Fatal(e)
	}
	run := thresholdNextRunLimits74(t, s, first, func(a *SettlementPlanAuthorizationInput) {
		if e := s.pool.QueryRow(ctx, `SELECT clock_timestamp()+interval '60 seconds'`).Scan(&a.ExpiresAt); e != nil {
			t.Fatal(e)
		}
	})
	generateErr := s.GenerateSettlementPlanRun(ctx, run.ID)
	if generateErr == nil {
		t.Fatal("last audit wait committed expired reserve authority", generateErr)
	}
	var entered bool
	if e := s.pool.QueryRow(ctx, `SELECT is_called FROM threshold_final_audit_entered74`).Scan(&entered); e != nil || !entered {
		t.Fatal("fixture failed before reaching the delayed final audit", generateErr, e)
	}
	var count, version int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM ledger_requests),(SELECT threshold_source_version FROM settlement_plan_runs WHERE id=$1)", run.ID).Scan(&count, &version); e != nil || count != 0 || version != 0 {
		t.Fatal("expired attempt left money or a sealed partial source", count, version, e)
	}
}
