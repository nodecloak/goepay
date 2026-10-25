package payment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func scanLedgerCounters78(t *testing.T, s *Store) (int64, int64) {
	t.Helper()
	report, e := s.CheckLedger(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(report)
	if e != nil {
		t.Fatal(e)
	}
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("invalid ledger report")
	}
	read := func(key string) int64 {
		value, present := fields[key]
		if !present || string(value) == "null" {
			t.Fatal("missing numeric scan recovery diagnostic", key)
		}
		var n int64
		if json.Unmarshal(value, &n) != nil {
			t.Fatal("non-numeric scan recovery diagnostic", key)
		}
		return n
	}
	return read("invalid_scan_recovery_sources"), read("unknown_scan_cancellations")
}

func TestScanRecovery78LedgerReportsEmptyAndQueuedSourcesWithoutFalseFinancialUnknown(t *testing.T) {
	s, _, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	if invalid, unknown := scanLedgerCounters78(t, s); invalid != 0 || unknown != 0 {
		t.Fatal("empty recovery is not a financial anomaly", invalid, unknown)
	}
	a, r, pending, _ := pendingReceiptProviderFixture78(t, s, o)
	if invalid, unknown := scanLedgerCounters78(t, s); invalid != 0 || unknown != 0 {
		t.Fatal("original send authority is not an unresolved cancellation", invalid, unknown)
	}
	if e := s.RecordScanPending(ctx, a.ID, "app78", r, pending); e != nil {
		t.Fatal(e)
	}
	if invalid, unknown := scanLedgerCounters78(t, s); invalid != 0 || unknown != 0 {
		t.Fatal("queued authenticated polling is not unresolved money", invalid, unknown)
	}
}

func TestScanRecovery78LedgerFindsIndependentAuditJobAndMissingParentAnchors(t *testing.T) {
	for _, mode := range []string{"origin-audit", "query-audit", "cancel-audit", "bad-job", "parent-missing"} {
		t.Run(mode, func(t *testing.T) {
			s, starter, o := recoveryOriginFixture78(t)
			ctx := context.Background()
			if mode == "parent-missing" {
				a, _, e := starter.BeginRecoverableScanAttempt(ctx, o.TradeNo, 7, "app78", "120000000000000078", "original-scope78")
				if e != nil {
					t.Fatal(e)
				}
				if _, e = s.pool.Exec(ctx, "ALTER TABLE scan_recovery_origins DISABLE TRIGGER USER"); e != nil {
					t.Fatal(e)
				}
				if _, e = s.pool.Exec(ctx, "DELETE FROM scan_recovery_origins WHERE attempt_id=$1", a.ID); e != nil {
					t.Fatal(e)
				}
			} else {
				query := map[string]string{
					"origin-audit": `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scan-recovery','scan-recovery-origins',90588,'create','{}')`,
					"query-audit":  `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scan-recovery','scan-recovery-queries',90589,'query','{"attempt_id":"90588"}')`,
					"cancel-audit": `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scan-recovery','scan-cancellations',90588,'claim','{}')`,
					"bad-job":      `INSERT INTO jobs(event_key,kind,payload)VALUES('scan-recover:bad','payment.scan-recover','{"attempt_id":"bad"}')`,
				}[mode]
				if _, e := s.pool.Exec(ctx, query); e != nil {
					t.Fatal(e)
				}
			}
			invalid, unknown := scanLedgerCounters78(t, s)
			wantUnknown := int64(0)
			if mode == "cancel-audit" {
				wantUnknown = 1
			}
			if invalid != 1 || unknown != wantUnknown {
				t.Fatal("independent original source lost from checker", mode, invalid, unknown)
			}
		})
	}
}

func TestScanRecovery78LedgerCoversAllPagesAndRejectsMalformedSourceNumbers(t *testing.T) {
	for _, mode := range []string{"pages", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _ := recoveryOriginFixture78(t)
			ctx := context.Background()
			if mode == "pages" {
				if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)SELECT 'system',0,'scan-recovery','scan-recovery-origins',100000+n,'create','{}' FROM generate_series(1,105)n;
   INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scan-recovery','scan-cancellations',100106,'claim','{}')`); e != nil {
					t.Fatal(e)
				}
				if invalid, unknown := scanLedgerCounters78(t, s); invalid != 106 || unknown != 1 {
					t.Fatal("checker stopped at its first page", invalid, unknown)
				}
			} else {
				for i, key := range []string{"", "001", "+1", "9223372036854775808", "-1"} {
					if _, e := s.pool.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload)VALUES('malformed-scan:'||$1::int::text,'payment.scan-recover',jsonb_build_object('attempt_id',$2::text))`, i, key); e != nil {
						t.Fatal(e)
					}
				}
				if invalid, unknown := scanLedgerCounters78(t, s); invalid != 5 || unknown != 0 {
					t.Fatal("malformed untrusted source numbers were accepted or overflowed", invalid, unknown)
				}
			}
		})
	}
}

func TestScanRecovery78LedgerCountsOnlyPermanentlyClaimedUnknownCancellation(t *testing.T) {
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
	scope, _ := adapter.TransferAccountScope(p)
	query, owner, e := s.ClaimScanQuery(ctx, job.ID, job.Token, scope)
	if e != nil || !owner {
		t.Fatal(e)
	}
	if invalid, unknown := scanLedgerCounters78(t, s); invalid != 0 || unknown != 0 {
		t.Fatal("unfinished read counted as unknown financial write", invalid, unknown)
	}
	result, e := p.ObserveScan(ctx, adapter.QueryRequest{TradeNo: query.TradeNo})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RecordScanQueryResult(ctx, query.ID, job.Token, result, nil); e != nil {
		t.Fatal(e)
	}
	for i := 2; i <= 6; i++ {
		waitScanQueryBoundary78(t, s, a.ID)
		query, owner, e = s.ClaimScanQuery(ctx, job.ID, job.Token, scope)
		if e != nil || !owner {
			t.Fatal(e)
		}
		result, e = p.ObserveScan(ctx, adapter.QueryRequest{TradeNo: query.TradeNo, ProviderTradeNo: query.ProviderTradeNo})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.RecordScanQueryResult(ctx, query.ID, job.Token, result, nil); e != nil {
			t.Fatal(e)
		}
	}
	if _, owner, e = s.ClaimScanCancellation(ctx, job.ID, job.Token, scope); e != nil || !owner {
		t.Fatal(e)
	}
	if invalid, unknown := scanLedgerCounters78(t, s); invalid != 0 || unknown != 1 {
		t.Fatal("committed no-ACK original cancellation omitted", invalid, unknown)
	}
	if e = s.RecordScanCancellation(ctx, a.ID, job.Token, adapter.ScanCancellationResult{}, errors.New("synthetic unknown78")); e != nil {
		t.Fatal(e)
	}
	if invalid, unknown := scanLedgerCounters78(t, s); invalid != 0 || unknown != 1 {
		t.Fatal("unknown original result was hidden or marked structurally forged", invalid, unknown)
	}
}
