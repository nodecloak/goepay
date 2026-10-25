package payment

import (
	"context"
	"errors"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func TestScanCancellation78PermanentBarrierCorruptionCannotBecomeLegacyIncome(t *testing.T) {
	s, _, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	a, r, pending, p := pendingReceiptProviderFixture78(t, s, o)
	if e := s.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordScanPending(ctx, a.ID, "app78", r, pending); e != nil {
		t.Fatal(e)
	}
	job, _ := claimScanFixtureJob78(t, s)
	sixPendingScanQueries78(t, s, a, p, job)
	scope, _ := adapter.TransferAccountScope(p)
	if _, owner, e := s.ClaimScanCancellation(ctx, job.ID, job.Token, scope); e != nil || !owner {
		t.Fatal(e)
	}
	for _, mode := range []string{"claim-deleted", "claim-proof", "claim-audit", "head-deleted", "source-proof", "account-downgrade", "query-receipt"} {
		t.Run(mode, func(t *testing.T) {
			tx, e := s.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			if _, e = tx.Exec(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", o.ID); e != nil {
				t.Fatal(e)
			}
			if _, e = tx.Exec(ctx, `ALTER TABLE scan_cancellation_claims DISABLE TRIGGER USER;ALTER TABLE scan_recovery_origins DISABLE TRIGGER USER;ALTER TABLE scan_recovery_query_heads DISABLE TRIGGER USER;ALTER TABLE scan_recovery_query_events DISABLE TRIGGER USER;ALTER TABLE provider_attempt_accounts DISABLE TRIGGER USER;ALTER TABLE backoffice_audit DISABLE TRIGGER USER`); e != nil {
				t.Fatal(e)
			}
			query := map[string]string{
				"claim-deleted":     `DELETE FROM scan_cancellation_claims WHERE attempt_id=$1`,
				"claim-proof":       `UPDATE scan_cancellation_claims SET proof_hash=decode(repeat('00',32),'hex') WHERE attempt_id=$1`,
				"claim-audit":       `DELETE FROM backoffice_audit WHERE resource='scan-cancellations' AND resource_id=$1`,
				"head-deleted":      `DELETE FROM scan_recovery_query_heads WHERE attempt_id=$1`,
				"source-proof":      `UPDATE scan_recovery_origins SET proof_hash=decode(repeat('00',32),'hex') WHERE attempt_id=$1`,
				"account-downgrade": `UPDATE provider_attempt_accounts SET recovery_version=0,recovery_source_hash=NULL WHERE attempt_id=$1`,
				"query-receipt":     `UPDATE scan_recovery_query_events SET receipt=jsonb_set(receipt,'{amount}','"124"') WHERE attempt_id=$1 AND kind='observed'`,
			}[mode]
			if _, e = tx.Exec(ctx, query, a.ID); e != nil {
				t.Fatal(e)
			}
			if e = s.checkScanCancellationBarrier(ctx, tx, o.ID); !errors.Is(e, ErrConflict) {
				t.Fatal("corrupt permanent barrier became payable", mode, e)
			}
			var report LedgerCheck
			if e = s.checkScanRecoveries(ctx, tx, &report); e != nil || report.InvalidScanRecoverySources != 1 || report.UnknownScanCancellations != 1 {
				t.Fatal("corrupt permanent barrier disappeared from diagnostics", mode, report.InvalidScanRecoverySources, report.UnknownScanCancellations, e)
			}
		})
	}
	if e := s.MarkPaid(ctx, o.TradeNo, "barrier-still-unknown78"); !errors.Is(e, ErrScanCancellationPending) {
		t.Fatal("rollback changed original valid barrier", e)
	}
	b, e := s.Balance(ctx, 78)
	if e != nil || b.Available != 0 {
		t.Fatal("corruption checks changed original funds", b, e)
	}
}

func TestScanCancellation78FinalAuditWaitCannotCommitStaleRevokedOrLeaseLostAuthority(t *testing.T) {
	for _, mode := range []string{"expired", "merchant-disabled", "lease-lost"} {
		t.Run(mode, func(t *testing.T) {
			s, _, o := recoveryOriginFixture78(t)
			ctx := context.Background()
			a, r, pending, p := pendingReceiptProviderFixture78(t, s, o)
			if e := s.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
				t.Fatal(e)
			}
			if e := s.RecordScanPending(ctx, a.ID, "app78", r, pending); e != nil {
				t.Fatal(e)
			}
			job, _ := claimScanFixtureJob78(t, s)
			sixPendingScanQueries78(t, s, a, p, job)
			scope, _ := adapter.TransferAccountScope(p)
			injection := `SELECT created_at+interval '3 seconds' INTO deadline FROM scan_recovery_query_events ORDER BY revision DESC LIMIT 1;PERFORM pg_sleep(GREATEST(0,extract(epoch FROM(deadline-clock_timestamp())))+0.05);`
			if mode == "merchant-disabled" {
				injection = `UPDATE merchants SET enabled=false WHERE id=78;`
			}
			if mode == "lease-lost" {
				injection = `UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE kind='payment.scan-recover';`
			}
			if _, e := s.pool.Exec(ctx, `CREATE SEQUENCE scan78_final_audit_seen;CREATE FUNCTION scan78_final_audit_delay() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE deadline timestamptz;BEGIN IF NEW.resource='scan-cancellations' AND NEW.operation='claim' THEN PERFORM nextval('scan78_final_audit_seen');`+injection+`END IF;RETURN NEW;END $$;CREATE TRIGGER scan78_final_audit_delay BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION scan78_final_audit_delay()`); e != nil {
				t.Fatal(e)
			}
			if _, owner, e := s.ClaimScanCancellation(ctx, job.ID, job.Token, scope); !errors.Is(e, ErrState) || owner {
				t.Fatal("authority changed during audit wait still granted reverse", mode, owner, e)
			}
			var reached bool
			var count int
			if e := s.pool.QueryRow(ctx, `SELECT (SELECT is_called FROM scan78_final_audit_seen),(SELECT count(*) FROM scan_cancellation_claims)+(SELECT count(*) FROM backoffice_audit WHERE resource='scan-cancellations')`).Scan(&reached, &count); e != nil || !reached || count != 0 {
				t.Fatal("final wait not reached or claim partially committed", reached, count, e)
			}
			flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}}
			if e := flow.HandleNotification(ctx, 7, o.TradeNo, nil, scanPaidCallback78(o)); e != nil {
				t.Fatal("rolled-back reverse swallowed original valid payment", e)
			}
			b, e := s.Balance(ctx, 78)
			if e != nil || b.Available != 123 {
				t.Fatal("original callback funds lost after rollback", b, e)
			}
		})
	}
}
