package payment

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/platform/jobs"
)

type scanQueryRecorder interface {
	ClaimScanQuery(context.Context, int64, string, string) (ScanQueryClaim, bool, error)
	RecordScanQueryResult(context.Context, int64, string, adapter.QueryResult, error) error
}

func claimScanFixtureJob78(t *testing.T, s *Store) (jobs.Job, jobs.Queue) {
	t.Helper()
	ctx := context.Background()
	q := jobs.Queue{Pool: s.pool}
	if _, e := s.pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM(run_at-clock_timestamp())))+0.02) FROM jobs WHERE kind='payment.scan-recover'`); e != nil {
		t.Fatal(e)
	}
	job, e := q.Claim(ctx, []string{"payment.scan-recover"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	return job, q
}
func waitScanQueryBoundary78(t *testing.T, s *Store, attemptID int64) {
	t.Helper()
	_, e := s.pool.Exec(context.Background(), `SELECT pg_sleep(GREATEST(0,extract(epoch FROM(created_at+interval '3 seconds'-clock_timestamp())))+0.02) FROM scan_recovery_query_events WHERE attempt_id=$1 ORDER BY revision DESC LIMIT 1`, attemptID)
	if e != nil {
		t.Fatal(e)
	}
}

func TestScanRecovery78SixAuthenticatedPendingReadsCannotAcquireASeventh(t *testing.T) {
	s, _, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	a, r, pending, p := pendingReceiptProviderFixture78(t, s, o)
	if e := s.RecordScanPending(ctx, a.ID, "app78", r, pending); e != nil {
		t.Fatal(e)
	}
	job, _ := claimScanFixtureJob78(t, s)
	scope, _ := adapter.TransferAccountScope(p)
	for number := 1; number <= 6; number++ {
		claim, owner, e := s.ClaimScanQuery(ctx, job.ID, job.Token, scope)
		if e != nil || !owner || claim.Sequence != number {
			t.Fatal("bounded query missing", number, owner, e)
		}
		result, e := p.ObserveScan(ctx, adapter.QueryRequest{TradeNo: claim.TradeNo, ProviderTradeNo: claim.ProviderTradeNo})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.RecordScanQueryResult(ctx, claim.ID, job.Token, result, nil); e != nil {
			t.Fatal(e)
		}
		if number < 6 {
			waitScanQueryBoundary78(t, s, a.ID)
		}
	}
	if _, owner, e := s.ClaimScanQuery(ctx, job.ID, job.Token, scope); !errors.Is(e, ErrState) || owner {
		t.Fatal("seventh automatic query granted", owner, e)
	}
	var count, revision int
	if e := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM scan_recovery_query_events),revision FROM scan_recovery_query_heads WHERE attempt_id=$1`, a.ID).Scan(&count, &revision); e != nil || count != 12 || revision != 12 {
		t.Fatal("six paired original observations not retained", count, revision, e)
	}
}

func TestScanRecovery78ReadErrorAndExpiredLeaseCannotBecomeAnotherQuery(t *testing.T) {
	for _, mode := range []string{"read-error", "lease-expired"} {
		t.Run(mode, func(t *testing.T) {
			s, _, o := recoveryOriginFixture78(t)
			ctx := context.Background()
			a, r, pending, p := pendingReceiptProviderFixture78(t, s, o)
			if e := s.RecordScanPending(ctx, a.ID, "app78", r, pending); e != nil {
				t.Fatal(e)
			}
			job, q := claimScanFixtureJob78(t, s)
			scope, _ := adapter.TransferAccountScope(p)
			claim, owner, e := s.ClaimScanQuery(ctx, job.ID, job.Token, scope)
			if e != nil || !owner {
				t.Fatal(e)
			}
			result, e := p.ObserveScan(ctx, adapter.QueryRequest{TradeNo: claim.TradeNo, ProviderTradeNo: claim.ProviderTradeNo})
			if e != nil {
				t.Fatal(e)
			}
			if mode == "read-error" {
				for i := 0; i < 2; i++ {
					if e = s.RecordScanQueryResult(ctx, claim.ID, job.Token, adapter.QueryResult{}, errors.New("synthetic read failure78")); e != nil {
						t.Fatal(e)
					}
				}
				if e = s.RecordScanQueryResult(ctx, claim.ID, job.Token, result, nil); !errors.Is(e, ErrConflict) {
					t.Fatal("failed read was upgraded to a new authenticated reply", e)
				}
				if _, owner, e = s.ClaimScanQuery(ctx, job.ID, job.Token, scope); !errors.Is(e, ErrState) || owner {
					t.Fatal("read failure continued automatic cancellation chain", owner, e)
				}
			} else {
				if _, e = s.pool.Exec(ctx, "UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", job.ID); e != nil {
					t.Fatal(e)
				}
				if e = s.RecordScanQueryResult(ctx, claim.ID, job.Token, result, nil); e == nil {
					t.Fatal("expired lease accepted observation")
				}
				newer, e := q.Claim(ctx, []string{"payment.scan-recover"}, time.Minute)
				if e != nil || newer.Token == job.Token {
					t.Fatal("lease fixture not reclaimed", e)
				}
				if _, owner, e = s.ClaimScanQuery(ctx, newer.ID, newer.Token, scope); !errors.Is(e, ErrConflict) || owner {
					t.Fatal("new lease reopened prior query claim", owner, e)
				}
			}
		})
	}
}

func TestScanRecovery78QueryHeadHistoryAndAuditCorruptionBlockProgress(t *testing.T) {
	for _, mode := range []string{"head-missing", "head-proof", "head-reset", "receipt", "audit-missing", "audit-orphan"} {
		t.Run(mode, func(t *testing.T) {
			s, _, o := recoveryOriginFixture78(t)
			ctx := context.Background()
			a, r, pending, p := pendingReceiptProviderFixture78(t, s, o)
			if e := s.RecordScanPending(ctx, a.ID, "app78", r, pending); e != nil {
				t.Fatal(e)
			}
			job, _ := claimScanFixtureJob78(t, s)
			scope, _ := adapter.TransferAccountScope(p)
			claim, owner, e := s.ClaimScanQuery(ctx, job.ID, job.Token, scope)
			if e != nil || !owner {
				t.Fatal(e)
			}
			result, e := p.ObserveScan(ctx, adapter.QueryRequest{TradeNo: claim.TradeNo, ProviderTradeNo: claim.ProviderTradeNo})
			if e != nil {
				t.Fatal(e)
			}
			if e = s.RecordScanQueryResult(ctx, claim.ID, job.Token, result, nil); e != nil {
				t.Fatal(e)
			}
			if _, e = s.pool.Exec(ctx, `ALTER TABLE scan_recovery_query_heads DISABLE TRIGGER USER;ALTER TABLE scan_recovery_query_events DISABLE TRIGGER USER;ALTER TABLE backoffice_audit DISABLE TRIGGER USER`); e != nil {
				t.Fatal(e)
			}
			query := map[string]string{
				"head-missing":  `DELETE FROM scan_recovery_query_heads WHERE attempt_id=$1`,
				"head-proof":    `UPDATE scan_recovery_query_heads SET proof_hash=decode(repeat('00',32),'hex') WHERE attempt_id=$1`,
				"head-reset":    `UPDATE scan_recovery_query_heads SET revision=0,event_id=NULL,event_hash=NULL WHERE attempt_id=$1`,
				"receipt":       `UPDATE scan_recovery_query_events SET receipt=jsonb_set(receipt,'{amount}','"124"') WHERE attempt_id=$1 AND kind='observed'`,
				"audit-missing": `DELETE FROM backoffice_audit WHERE resource='scan-recovery-queries' AND changes->>'attempt_id'=$1::bigint::text`,
				"audit-orphan":  `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scan-recovery','scan-recovery-queries',999999,'query',jsonb_build_object('attempt_id',$1::bigint::text))`,
			}[mode]
			if _, e = s.pool.Exec(ctx, query, a.ID); e != nil {
				t.Fatal(e)
			}
			if _, owner, e = s.ClaimScanQuery(ctx, job.ID, job.Token, scope); !errors.Is(e, ErrConflict) || owner {
				t.Fatal("damaged history granted a new query", mode, owner, e)
			}
		})
	}
}

func TestScanRecovery78QueryAuditFailureRollsBackProgressAndAllowsSameReceipt(t *testing.T) {
	s, _, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	a, r, pending, p := pendingReceiptProviderFixture78(t, s, o)
	if e := s.RecordScanPending(ctx, a.ID, "app78", r, pending); e != nil {
		t.Fatal(e)
	}
	job, _ := claimScanFixtureJob78(t, s)
	scope, _ := adapter.TransferAccountScope(p)
	claim, owner, e := s.ClaimScanQuery(ctx, job.ID, job.Token, scope)
	if e != nil || !owner {
		t.Fatal(e)
	}
	result, e := p.ObserveScan(ctx, adapter.QueryRequest{TradeNo: claim.TradeNo, ProviderTradeNo: claim.ProviderTradeNo})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION reject_scan_query78()RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='scan-recovery-queries' AND NEW.operation='observed' THEN RAISE EXCEPTION 'synthetic query audit failure78';END IF;RETURN NEW;END $$;CREATE TRIGGER reject_scan_query78 BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_scan_query78()`); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordScanQueryResult(ctx, claim.ID, job.Token, result, nil); e == nil {
		t.Fatal("query audit failure still advanced original head")
	}
	var count, revision int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM scan_recovery_query_events),revision FROM scan_recovery_query_heads WHERE attempt_id=$1`, a.ID).Scan(&count, &revision); e != nil || count != 1 || revision != 1 {
		t.Fatal("partial observation committed", count, revision, e)
	}
	if _, e = s.pool.Exec(ctx, "DROP TRIGGER reject_scan_query78 ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordScanQueryResult(ctx, claim.ID, job.Token, result, nil); e != nil {
		t.Fatal("original receipt could not recover audit rollback", e)
	}
}

func TestScanRecovery78QueryClaimIsCommittedOnceAndRequiresOriginalLease(t *testing.T) {
	s, _, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	runner, ok := any(s).(scanQueryRecorder)
	if !ok {
		t.Fatal("bounded original query claim missing")
	}
	a, request, pending, p := pendingReceiptProviderFixture78(t, s, o)
	if e := s.RecordScanPending(ctx, a.ID, "app78", request, pending); e != nil {
		t.Fatal(e)
	}
	scope, _ := adapter.TransferAccountScope(p)
	// Wait for the real persisted first-query boundary; never backdate authority.
	if _, e := s.pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM(run_at-clock_timestamp())))+0.02) FROM jobs WHERE kind='payment.scan-recover'`); e != nil {
		t.Fatal(e)
	}
	q := jobs.Queue{Pool: s.pool}
	job, e := q.Claim(ctx, []string{"payment.scan-recover"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e := runner.ClaimScanQuery(ctx, job.ID, "wrong-lease78", scope); e == nil {
		t.Fatal("wrong job lease granted a query")
	}
	if _, _, e := runner.ClaimScanQuery(ctx, job.ID, job.Token, "changed-account78"); e == nil {
		t.Fatal("changed provider account granted a query")
	}
	var owners, failures atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			claim, owner, e := runner.ClaimScanQuery(ctx, job.ID, job.Token, scope)
			if e != nil || claim.AttemptID != a.ID || claim.Sequence != 1 || claim.TradeNo != o.TradeNo || claim.Amount != 123 {
				failures.Add(1)
			}
			if owner {
				owners.Add(1)
			}
		}()
	}
	group.Wait()
	if owners.Load() != 1 || failures.Load() != 0 {
		t.Fatal("concurrent original poll acquired duplicate reads", owners.Load(), failures.Load())
	}
	claim, owner, e := runner.ClaimScanQuery(ctx, job.ID, job.Token, scope)
	if e != nil || owner {
		t.Fatal("same lease regained original read opportunity", owner, e)
	}
	result, e := p.ObserveScan(ctx, adapter.QueryRequest{TradeNo: claim.TradeNo, ProviderTradeNo: claim.ProviderTradeNo})
	if e != nil {
		t.Fatal(e)
	}
	fake := adapter.QueryResult{TradeNo: o.TradeNo, ProviderTradeNo: "UP78_LOCAL", State: "USERPAYING", Currency: "CNY", Amount: 123}
	if e := runner.RecordScanQueryResult(ctx, claim.ID, job.Token, fake, nil); e == nil {
		t.Fatal("caller-built query advanced bounded cancellation eligibility")
	}
	if e := runner.RecordScanQueryResult(ctx, claim.ID, "wrong-lease78", result, nil); e == nil {
		t.Fatal("unrelated lease saved original observation")
	}
	for i := 0; i < 2; i++ {
		if e := runner.RecordScanQueryResult(ctx, claim.ID, job.Token, result, nil); e != nil {
			t.Fatal("original observation not idempotent", e)
		}
	}
	var events int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM scan_recovery_query_events").Scan(&events); e != nil || events != 2 {
		t.Fatal("query claim/result missing or duplicated", events, e)
	}
	if _, owner, e := runner.ClaimScanQuery(ctx, job.ID, job.Token, scope); e == nil || owner {
		t.Fatal("second query skipped persisted three-second interval", owner, e)
	}
	b, e := s.Balance(ctx, 78)
	if e != nil || b.Available != 0 {
		t.Fatal("pending poll credited money", b, e)
	}
}
