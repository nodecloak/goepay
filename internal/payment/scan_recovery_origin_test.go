package payment

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type recoverableScanBeginner interface {
	BeginRecoverableScanAttempt(context.Context, string, int64, string, string, string) (ProviderAttempt, bool, error)
}

func recoveryOriginFixture78(t *testing.T) (*Store, recoverableScanBeginner, Order) {
	t.Helper()
	return recoveryOriginFixtureProvider78(t, "wxpay")
}
func recoveryOriginFixtureProvider78(t *testing.T, provider string) (*Store, recoverableScanBeginner, Order) {
	t.Helper()
	s := testStore(t)
	starter, ok := any(s).(recoverableScanBeginner)
	if !ok {
		t.Fatal("original scan has no atomic recovery source")
	}
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 78, Name: "Local recovery source", APIKey: "synthetic78"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 78, OutTradeNo: "original78", Name: "Local barcode", Type: provider, Amount: 123})
	if e != nil {
		t.Fatal(e)
	}
	return s, starter, o
}

func TestScanRecovery78OriginCommitsOnceWithOriginalSendAndNeverStoresBarcode(t *testing.T) {
	s, starter, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	var owned atomic.Int32
	var failures atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, owner, e := starter.BeginRecoverableScanAttempt(ctx, o.TradeNo, 7, "app78", "120000000000000078", "original-scope78")
			if e != nil {
				failures.Add(1)
			}
			if owner {
				owned.Add(1)
			}
		}()
	}
	group.Wait()
	if owned.Load() != 1 || failures.Load() != 0 {
		t.Fatal("source allocated more than one send", owned.Load(), failures.Load())
	}
	a, e := s.ProviderAttempt(ctx, o.TradeNo)
	if e != nil {
		t.Fatal(e)
	}
	var origins, accounts, identities, audits, queries int
	var raw string
	e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM scan_recovery_origins),(SELECT count(*) FROM provider_attempt_accounts WHERE recovery_version=1),(SELECT count(*) FROM provider_attempt_identities),(SELECT count(*) FROM backoffice_audit WHERE resource='scan-recovery-origins'),(SELECT count(*) FROM jobs WHERE kind='payment.reconcile'),(SELECT row_to_json(r)::text FROM scan_recovery_origins r WHERE attempt_id=$1)`, a.ID).Scan(&origins, &accounts, &identities, &audits, &queries, &raw)
	if e != nil || origins != 1 || accounts != 1 || identities != 1 || audits != 1 || queries != 1 || strings.Contains(raw, "120000000000000078") || strings.Contains(raw, "original-scope78") {
		t.Fatal("source missing, duplicated, or secret persisted", e, origins, accounts, identities, audits, queries)
	}
	if e = s.checkProviderAccountScope(ctx, s.pool, a.ID, "original-scope78", true); e != nil {
		t.Fatal("original account refused", e)
	}
	conn, e := s.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(ctx, "SET bytea_output='escape'"); e != nil {
		conn.Release()
		t.Fatal(e)
	}
	checkErr := s.checkProviderAccountScope(ctx, conn, a.ID, "original-scope78", true)
	_, resetErr := conn.Exec(ctx, "RESET bytea_output")
	conn.Release()
	if checkErr != nil || resetErr != nil {
		t.Fatal("new recovery account depends on hex bytea output", checkErr, resetErr)
	}
	for _, changed := range []struct{ app, code, scope string }{{"other78", "120000000000000078", "original-scope78"}, {"app78", "120000000000000079", "original-scope78"}, {"app78", "120000000000000078", "changed-scope78"}} {
		if _, _, e = starter.BeginRecoverableScanAttempt(ctx, o.TradeNo, 7, changed.app, changed.code, changed.scope); !errors.Is(e, ErrConflict) {
			t.Fatal("changed origin accepted", e)
		}
	}
	b, e := s.Balance(ctx, 78)
	if e != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatal("recovery origin changed funds", b, e)
	}
}

func TestScanRecovery78CannotBackfillHistoricalUnknownAttempt(t *testing.T) {
	s, starter, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	a, owner, e := s.BeginProviderAttemptWithAccount(ctx, o.TradeNo, 7, "scan", "app78", "120000000000000078", "original-scope78")
	if e != nil || !owner {
		t.Fatal(e)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
		t.Fatal(e)
	}
	if _, _, e = starter.BeginRecoverableScanAttempt(ctx, o.TradeNo, 7, "app78", "120000000000000078", "original-scope78"); !errors.Is(e, ErrConflict) {
		t.Fatal("historical scan granted new cancellation source", e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM scan_recovery_origins").Scan(&count); e != nil || count != 0 {
		t.Fatal("historical origin invented", count, e)
	}
	if e = s.checkProviderAccountScope(ctx, s.pool, a.ID, "original-scope78", true); e != nil {
		t.Fatal("legacy query account changed", e)
	}
}

func TestScanRecovery78OriginCannotBeDeletedDowngradedOrDetachedFromAudit(t *testing.T) {
	for _, mode := range []string{"source-deleted", "source-proof", "source-amount", "account-downgrade", "audit-deleted", "audit-extra"} {
		t.Run(mode, func(t *testing.T) {
			s, starter, o := recoveryOriginFixture78(t)
			ctx := context.Background()
			a, _, e := starter.BeginRecoverableScanAttempt(ctx, o.TradeNo, 7, "app78", "120000000000000078", "original-scope78")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.pool.Exec(ctx, `ALTER TABLE scan_recovery_origins DISABLE TRIGGER USER;ALTER TABLE provider_attempt_accounts DISABLE TRIGGER USER;ALTER TABLE backoffice_audit DISABLE TRIGGER USER`); e != nil {
				t.Fatal(e)
			}
			query := map[string]string{"source-deleted": "DELETE FROM scan_recovery_origins WHERE attempt_id=$1", "source-proof": "UPDATE scan_recovery_origins SET proof_hash=decode(repeat('00',32),'hex') WHERE attempt_id=$1", "source-amount": "UPDATE scan_recovery_origins SET amount=124 WHERE attempt_id=$1", "account-downgrade": "UPDATE provider_attempt_accounts SET recovery_version=0,recovery_source_hash=NULL WHERE attempt_id=$1", "audit-deleted": "DELETE FROM backoffice_audit WHERE resource='scan-recovery-origins' AND resource_id=$1", "audit-extra": "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation)VALUES('system',0,'scan-recovery','scan-recovery-origins',$1,'reopen')"}[mode]
			if _, e = s.pool.Exec(ctx, query, a.ID); e != nil {
				t.Fatal(e)
			}
			if e = s.checkProviderAccountScope(ctx, s.pool, a.ID, "original-scope78", true); !errors.Is(e, ErrConflict) {
				t.Fatal("damaged recovery downgraded to ordinary payable scan", mode, e)
			}
		})
	}
}

func TestScanRecovery78Actual77AccountMACAndUnknownOrderRemainLegacyWithoutBackfill(t *testing.T) {
	s, facts := actualHistoricalStore155(t, "scan77")
	ctx := context.Background()
	var o Order
	var a ProviderAttempt
	facts.decode(t, "order", &o)
	facts.decode(t, "attempt", &a)
	var e error
	var before, after string
	snapshot := `SELECT jsonb_build_array((SELECT to_jsonb(a)-'recovery_version'-'recovery_source_hash' FROM provider_attempt_accounts a WHERE attempt_id=$1),(SELECT to_jsonb(p) FROM provider_attempts p WHERE id=$1),(SELECT to_jsonb(i) FROM provider_attempt_identities i WHERE attempt_id=$1),(SELECT to_jsonb(o) FROM orders o WHERE id=$2))::text`
	if e = s.pool.QueryRow(ctx, snapshot, a.ID, o.ID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../db/migrations/000078_scan_recovery_origins.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	if e = s.pool.QueryRow(ctx, snapshot, a.ID, o.ID).Scan(&after); e != nil || after != before {
		t.Fatal("78 changed legacy original fields", e)
	}
	var sources, version int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM scan_recovery_origins),recovery_version FROM provider_attempt_accounts WHERE attempt_id=$1`, a.ID).Scan(&sources, &version); e != nil || sources != 0 || version != 0 {
		t.Fatal("legacy recovery source invented", sources, version, e)
	}
	if e = s.checkProviderAccountScope(ctx, s.pool, a.ID, "original-scope78", true); e != nil {
		t.Fatal("legacy query account broken after upgrade", e)
	}
	if _, _, e = s.BeginRecoverableScanAttempt(ctx, o.TradeNo, 7, "app78", "120000000000000078", "original-scope78"); !errors.Is(e, ErrConflict) {
		t.Fatal("upgrade retroactively granted cancellation", e)
	}
}

func TestScanRecovery78SourceInsertFailureRollsBackOriginalSendOpportunity(t *testing.T) {
	s, starter, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION reject_scan78() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic source failure78';END $$;CREATE TRIGGER reject_scan78 BEFORE INSERT ON scan_recovery_origins FOR EACH ROW EXECUTE FUNCTION reject_scan78()`); e != nil {
		t.Fatal(e)
	}
	if _, _, e := starter.BeginRecoverableScanAttempt(ctx, o.TradeNo, 7, "app78", "120000000000000078", "original-scope78"); e == nil {
		t.Fatal("source failure still granted send")
	}
	var count int
	var status string
	if e := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_attempts)+(SELECT count(*) FROM provider_attempt_accounts)+(SELECT count(*) FROM provider_attempt_identities)+(SELECT count(*) FROM scan_recovery_origins)+(SELECT count(*) FROM jobs)+(SELECT count(*) FROM backoffice_audit WHERE resource='scan-recovery-origins'),status FROM orders WHERE id=$1`, o.ID).Scan(&count, &status); e != nil || count != 0 || status != "created" {
		t.Fatal("partial source committed", count, status, e)
	}
	if _, e := s.pool.Exec(ctx, "DROP TRIGGER reject_scan78 ON scan_recovery_origins"); e != nil {
		t.Fatal(e)
	}
	if _, owner, e := starter.BeginRecoverableScanAttempt(ctx, o.TradeNo, 7, "app78", "120000000000000078", "original-scope78"); e != nil || !owner {
		t.Fatal("rollback consumed send opportunity", owner, e)
	}
}
