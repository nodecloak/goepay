package payment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func TestScanRecovery78ReadViewUsesVerifiedOriginalHistoryAndMerchantOwnership(t *testing.T) {
	s, _, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	if view, e := s.ScanRecoveryStatus(ctx, 78, o.TradeNo); e != nil || view != nil {
		t.Fatal("unsubmitted order gained recovery status", e)
	}
	a, r, pending, p := pendingReceiptProviderStateFixture78(t, s, o, func() string { return "NOTPAY" })
	if e := s.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
		t.Fatal(e)
	}
	if view, e := s.ScanRecoveryStatus(ctx, 78, o.TradeNo); e != nil || view == nil || view.State != "submitted" || view.Queries != 0 || view.MaximumQueries != 6 {
		t.Fatal("original source status missing", view, e)
	}
	if _, e := s.ScanRecoveryStatus(ctx, 79, o.TradeNo); !errors.Is(e, ErrNotFound) {
		t.Fatal("different merchant could inspect original recovery", e)
	}
	if e := s.RecordScanPending(ctx, a.ID, "app78", r, pending); e != nil {
		t.Fatal(e)
	}
	view, e := s.ScanRecoveryStatus(ctx, 78, o.TradeNo)
	if e != nil || view.State != "waiting" || view.RequiresReconciliation || view.NextQueryAt == nil {
		t.Fatal("queued original recovery mislabeled", view, e)
	}
	raw, e := json.Marshal(view)
	if e != nil || strings.Contains(string(raw), r.AuthCode) || strings.Contains(string(raw), "proof") || strings.Contains(string(raw), "scope") || strings.Contains(string(raw), "mch78") {
		t.Fatal("recovery view exposed bearer or internal authority", e)
	}
	job, _ := claimScanFixtureJob78(t, s)
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, ScanRecoveryEnabled: true}
	if _, e = flow.ExecuteScanRecovery(ctx, job.ID, job.Token); e != nil {
		t.Fatal(e)
	}
	view, e = s.ScanRecoveryStatus(ctx, 78, o.TradeNo)
	if e != nil || view.State != "stopped" || view.Queries != 1 || view.LastProviderState != "NOTPAY" || view.NextQueryAt != nil {
		t.Fatal("terminal original observation misrepresented", view, e)
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE scan_recovery_query_heads DISABLE TRIGGER USER;UPDATE scan_recovery_query_heads SET proof_hash=decode(repeat('00',32),'hex')`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ScanRecoveryStatus(ctx, 78, o.TradeNo); !errors.Is(e, ErrConflict) {
		t.Fatal("console accepted corrupt original query history", e)
	}
}
