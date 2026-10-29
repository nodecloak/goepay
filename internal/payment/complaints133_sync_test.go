package payment

import (
	"context"
	"errors"
	"strings"
	"testing"

	provider "goepay/internal/adapters/payment"
)

// upsertComplaint133 is idempotent by (pay_type, third_id): a re-sync of the
// same provider complaint id updates status/last-seen forward and never
// duplicates or rewrites identity/amount. RecordComplaintSyncResult implements
// the original MAX_FAILURES auto-stop: consecutive failures disable the task
// and record a single admin notice; a success resets the counter.
func TestComplaintUpsertIsIdempotentAndForwardOnly(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	amt := int64(1200)
	rec := provider.ComplaintRecord{
		ThirdID: "WX-UPSERT-1", PayType: "wxpay", Type: "PENDING", Status: "PENDING",
		Content: "未收到商品", AmountCents: &amt, Times: 1, TradeNos: []string{"T-NOPE"},
	}
	if e := s.upsertComplaint133(ctx, 7001, rec); e != nil {
		t.Fatal("first upsert:", e)
	}
	// Re-sync the same complaint with an advanced status.
	rec.Status, rec.Type = "PROCESSING", "PROCESSING"
	if e := s.upsertComplaint133(ctx, 7001, rec); e != nil {
		t.Fatal("second upsert:", e)
	}
	var count int
	var state, providerStatus string
	var amount *int64
	if e := s.pool.QueryRow(ctx, `SELECT count(*) OVER(),local_state,provider_status,amount_cents FROM complaints133 WHERE pay_type='wxpay' AND third_id='WX-UPSERT-1'`).Scan(&count, &state, &providerStatus, &amount); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.pool.Exec(context.Background(), `DELETE FROM complaints133 WHERE third_id='WX-UPSERT-1'`) })
	if count != 1 {
		t.Fatalf("duplicate rows: %d", count)
	}
	if state != "processing" || providerStatus != "PROCESSING" {
		t.Fatalf("status not advanced: state=%s provider=%s", state, providerStatus)
	}
	if amount == nil || *amount != 1200 {
		t.Fatalf("amount not preserved: %v", amount)
	}
}

func TestComplaintSyncFailuresStopTaskAtMax(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `INSERT INTO complaint_tasks133(channel_id,enabled,frequency,num) VALUES (7100,true,6,20)`); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM complaint_tasks133 WHERE channel_id=7100`)
	})
	boom := errors.New("provider sync failed")
	for i := 0; i < complaintMaxFailures-1; i++ {
		if e := s.RecordComplaintSyncResult(ctx, 7100, boom); e != nil {
			t.Fatal(e)
		}
	}
	var enabled bool
	var failures int
	if e := s.pool.QueryRow(ctx, `SELECT enabled,consecutive_failures FROM complaint_tasks133 WHERE channel_id=7100`).Scan(&enabled, &failures); e != nil {
		t.Fatal(e)
	}
	if !enabled || failures != complaintMaxFailures-1 {
		t.Fatalf("before threshold: enabled=%v failures=%d", enabled, failures)
	}
	// The failure that reaches the threshold disables the task + records an alert.
	if e := s.RecordComplaintSyncResult(ctx, 7100, boom); e != nil {
		t.Fatal(e)
	}
	if e := s.pool.QueryRow(ctx, `SELECT enabled,consecutive_failures FROM complaint_tasks133 WHERE channel_id=7100`).Scan(&enabled, &failures); e != nil {
		t.Fatal(e)
	}
	if enabled || failures != complaintMaxFailures {
		t.Fatalf("at threshold: enabled=%v failures=%d (want disabled, %d)", enabled, failures, complaintMaxFailures)
	}
	var alertReason string
	if e := s.pool.QueryRow(ctx, `SELECT COALESCE(last_reason,'') FROM complaint_tasks133 WHERE channel_id=7100`).Scan(&alertReason); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(alertReason, "自动停用") {
		t.Fatalf("auto-stop notice not recorded: %q", alertReason)
	}
	// A later success re-enables accounting (counter reset) when the task is on.
	s.pool.Exec(ctx, `UPDATE complaint_tasks133 SET enabled=true WHERE channel_id=7100`)
	if e := s.RecordComplaintSyncResult(ctx, 7100, nil); e != nil {
		t.Fatal(e)
	}
	if e := s.pool.QueryRow(ctx, `SELECT consecutive_failures FROM complaint_tasks133 WHERE channel_id=7100`).Scan(&failures); e != nil {
		t.Fatal(e)
	}
	if failures != 0 {
		t.Fatalf("success did not reset failures: %d", failures)
	}
}
