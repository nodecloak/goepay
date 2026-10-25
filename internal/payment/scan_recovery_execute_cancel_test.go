package payment

import (
	"context"
	"errors"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

type cancellationResponse78 struct {
	*adapter.Alipay
	lost         bool
	cancelCaller context.CancelFunc
}

func (p *cancellationResponse78) CancelScan(ctx context.Context, r adapter.ScanCancellationRequest) (adapter.ScanCancellationResult, error) {
	result, e := p.Alipay.CancelScan(ctx, r)
	if p.cancelCaller != nil {
		p.cancelCaller()
	}
	if p.lost {
		return adapter.ScanCancellationResult{}, &adapter.UnknownOutcome{Operation: "local cancel78", Cause: errors.New("synthetic lost original acknowledgement")}
	}
	return result, e
}

func TestScanRecovery78ExecutorSixPendingReadsCancelOriginalOnceAndKeepLateACK(t *testing.T) {
	for _, mode := range []string{"known", "lost", "caller-cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, _, o := recoveryOriginFixtureProvider78(t, "alipay")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fixture := alipayScanFixture78(t, s, o, "close")
			p := &cancellationResponse78{Alipay: fixture.provider, lost: mode == "lost"}
			if mode == "caller-cancelled" {
				p.cancelCaller = cancel
			}
			if e := s.RecordProviderAttemptResult(ctx, fixture.attempt.ID, "unknown", "", "", ""); e != nil {
				t.Fatal(e)
			}
			if e := s.RecordScanPending(ctx, fixture.attempt.ID, "app78", fixture.request, fixture.pending); e != nil {
				t.Fatal(e)
			}
			job, _ := claimScanFixtureJob78(t, s)
			flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true}
			enableScanRecovery78(t, &flow)
			for i := 1; i <= 6; i++ {
				delay, e := flow.ExecuteScanRecovery(ctx, job.ID, job.Token)
				if e != nil {
					t.Fatal("bounded original recovery failed", i, e)
				}
				if i < 6 {
					if delay <= 0 || fixture.cancellations.Load() != 0 {
						t.Fatal("early cancel or missing query interval", i, delay)
					}
					waitScanQueryBoundary78(t, s, fixture.attempt.ID)
				} else if delay != 0 {
					t.Fatal("sixth query did not end automatic chain", delay)
				}
			}
			if fixture.cancellations.Load() != 1 {
				t.Fatal("original cancellation was omitted or repeated", fixture.cancellations.Load())
			}
			// The original financial response must survive caller cancellation.
			readCtx := context.Background()
			current, e := s.OrderByTradeNo(readCtx, o.TradeNo)
			wantStatus := "closed"
			wantUnknown := int64(0)
			if mode == "lost" {
				wantStatus = "unknown"
				wantUnknown = 1
			}
			if e != nil || current.Status != wantStatus {
				t.Fatal("wrong original cancel outcome", current.Status, e)
			}
			if delay, e := flow.ExecuteScanRecovery(readCtx, job.ID, job.Token); e != nil || delay != 0 || fixture.cancellations.Load() != 1 {
				t.Fatal("recovery replay resent cancellation", delay, e, fixture.cancellations.Load())
			}
			if invalid, unknown := scanLedgerCounters78(t, s); invalid != 0 || unknown != wantUnknown {
				t.Fatal("original finality missing from diagnostics", invalid, unknown)
			}
			view, e := s.ScanRecoveryStatus(readCtx, 78, o.TradeNo)
			wantView := "cancelled"
			if mode == "lost" {
				wantView = "cancel_pending"
			}
			if e != nil || view == nil || view.State != wantView || view.Queries != 6 || view.RequiresReconciliation != (mode == "lost") {
				t.Fatal("console lost original financial finality", view, e)
			}
			balance, e := s.Balance(readCtx, 78)
			if e != nil || balance.Available != 0 || balance.Reserved != 0 {
				t.Fatal("cancel flow created funds", balance, e)
			}
		})
	}
}
