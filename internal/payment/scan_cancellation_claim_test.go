package payment

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/platform/jobs"
)

type scanCancellationRecorder interface {
	ClaimScanCancellation(context.Context, int64, string, string) (ScanCancellationClaim, bool, error)
	RecordScanCancellation(context.Context, int64, string, adapter.ScanCancellationResult, error) error
}

func scanPaidCallback78(o Order) []byte {
	fields := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app78", "mch_id": "mch78", "out_trade_no": o.TradeNo, "transaction_id": "UP78_LOCAL", "total_fee": "123", "fee_type": "CNY", "openid": "local-buyer78", "nonce_str": "callback78"}
	fields["sign"], _ = adapter.SignWechatV2(fields, "synthetic-pending-source-api-key78", "MD5")
	var body bytes.Buffer
	body.WriteString("<xml>")
	for field, value := range fields {
		fmt.Fprintf(&body, "<%s>", field)
		xml.EscapeText(&body, []byte(value))
		fmt.Fprintf(&body, "</%s>", field)
	}
	body.WriteString("</xml>")
	return body.Bytes()
}
func sixPendingScanQueries78(t *testing.T, s *Store, a ProviderAttempt, p adapter.Provider, job jobs.Job) {
	t.Helper()
	ctx := context.Background()
	scope, _ := adapter.TransferAccountScope(p)
	observer, ok := p.(adapter.ScanObservationProvider)
	if !ok {
		t.Fatal("original provider has no authenticated query")
	}
	for i := 1; i <= 6; i++ {
		claim, owner, e := s.ClaimScanQuery(ctx, job.ID, job.Token, scope)
		if e != nil || !owner {
			t.Fatal("original query unavailable", i, owner, e)
		}
		result, e := observer.ObserveScan(ctx, adapter.QueryRequest{TradeNo: claim.TradeNo, ProviderTradeNo: claim.ProviderTradeNo})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.RecordScanQueryResult(ctx, claim.ID, job.Token, result, nil); e != nil {
			t.Fatal(e)
		}
		if i < 6 {
			waitScanQueryBoundary78(t, s, a.ID)
		}
	}
}

func TestScanCancellation78PermanentClaimPrecedesAllLateIncomePathsAndNeverResends(t *testing.T) {
	s, _, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	canceller, ok := any(s).(scanCancellationRecorder)
	if !ok {
		t.Fatal("original scan lacks permanent cancellation barrier")
	}
	var state atomic.Value
	state.Store("USERPAYING")
	a, r, pending, p := pendingReceiptProviderStateFixture78(t, s, o, func() string { return state.Load().(string) })
	if e := s.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordScanPending(ctx, a.ID, "app78", r, pending); e != nil {
		t.Fatal(e)
	}
	job, _ := claimScanFixtureJob78(t, s)
	scope, _ := adapter.TransferAccountScope(p)
	if _, owner, e := canceller.ClaimScanCancellation(ctx, job.ID, job.Token, scope); e == nil || owner {
		t.Fatal("cancellation skipped six authenticated reads", owner, e)
	}
	sixPendingScanQueries78(t, s, a, p, job)
	var owners, failures atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			claim, owner, e := canceller.ClaimScanCancellation(ctx, job.ID, job.Token, scope)
			if e != nil || claim.AttemptID != a.ID || claim.Amount != 123 || claim.ProviderTradeNo != "UP78_LOCAL" {
				failures.Add(1)
			}
			if owner {
				owners.Add(1)
			}
		}()
	}
	group.Wait()
	if owners.Load() != 1 || failures.Load() != 0 {
		t.Fatal("permanent cancellation claim duplicated", owners.Load(), failures.Load())
	}
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true}
	if e := flow.HandleNotification(ctx, 7, o.TradeNo, nil, scanPaidCallback78(o)); !errors.Is(e, ErrScanCancellationPending) {
		t.Fatal("late signed callback bypassed cancellation barrier", e)
	}
	state.Store("SUCCESS")
	if _, e := flow.Query(ctx, 78, o.TradeNo); !errors.Is(e, ErrScanCancellationPending) {
		t.Fatal("late success query bypassed cancellation barrier", e)
	}
	if e := s.MarkPaid(ctx, o.TradeNo, "late-internal78"); !errors.Is(e, ErrScanCancellationPending) {
		t.Fatal("internal credit bypassed cancellation barrier", e)
	}
	if e := flow.Close(ctx, 78, o.TradeNo); !errors.Is(e, ErrScanCancellationPending) {
		t.Fatal("ordinary close bypassed pending reverse", e)
	}
	fake := adapter.ScanCancellationResult{TradeNo: o.TradeNo, ProviderTradeNo: "UP78_LOCAL", Amount: 123, State: "cancelled", Action: "close_or_refund"}
	if e := canceller.RecordScanCancellation(ctx, a.ID, job.Token, fake, nil); e == nil {
		t.Fatal("unsigned cancellation closed original payment")
	}
	if e := canceller.RecordScanCancellation(ctx, a.ID, job.Token, adapter.ScanCancellationResult{}, errors.New("synthetic cancellation outcome unknown78")); e != nil {
		t.Fatal(e)
	}
	if _, owner, e := canceller.ClaimScanCancellation(ctx, job.ID, job.Token, scope); e != nil || owner {
		t.Fatal("unknown cancellation granted another send", owner, e)
	}
	b, e := s.Balance(ctx, 78)
	if e != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatal("late success credited unknown reverse", b, e)
	}
	var journals, closes int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM journals),(SELECT count(*) FROM provider_operations WHERE kind='close')`).Scan(&journals, &closes); e != nil || journals != 0 || closes != 0 {
		t.Fatal("barrier allowed financial side effects", journals, closes, e)
	}
}

func TestScanCancellation78AlreadyPaidOrderCannotAcquireAReverse(t *testing.T) {
	s, _, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	canceller, ok := any(s).(scanCancellationRecorder)
	if !ok {
		t.Fatal("original cancellation claim missing")
	}
	a, r, pending, p := pendingReceiptProviderFixture78(t, s, o)
	if e := s.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordScanPending(ctx, a.ID, "app78", r, pending); e != nil {
		t.Fatal(e)
	}
	job, _ := claimScanFixtureJob78(t, s)
	sixPendingScanQueries78(t, s, a, p, job)
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}}
	if e := flow.HandleNotification(ctx, 7, o.TradeNo, nil, scanPaidCallback78(o)); e != nil {
		t.Fatal("valid original payment did not win order lock", e)
	}
	scope, _ := adapter.TransferAccountScope(p)
	if _, owner, e := canceller.ClaimScanCancellation(ctx, job.ID, job.Token, scope); !errors.Is(e, ErrState) || owner {
		t.Fatal("paid order acquired reverse authority", owner, e)
	}
	b, e := s.Balance(ctx, 78)
	if e != nil || b.Available != 123 {
		t.Fatal("winning original payment lost funds", b, e)
	}
}

func TestScanCancellation78GuardReadsActualLegacyShapeBeforeAccountProofs(t *testing.T) {
	s := membership72HistoricalStore(t, 45)
	if e := s.checkScanCancellationBarrier(context.Background(), s.pool, 1); e != nil {
		t.Fatal("new guard broke actual pre46 shape without any recovery source", e)
	}
}
