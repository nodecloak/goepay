package payment

import (
	"context"
	"errors"
	provider "goepay/internal/adapters/payment"
	"sync"
	"sync/atomic"
	"testing"
)

type manualCaptureProvider struct {
	releaseFlowProvider
	writes              atomic.Int32
	captureFailure      error
	beforeAuthorization func()
	wrongFreeze         bool
	queryFailure        error
	queryWaiting        bool
	mu                  sync.Mutex
}

func (p *manualCaptureProvider) Query(ctx context.Context, r provider.QueryRequest) (provider.QueryResult, error) {
	if p.queryFailure != nil {
		return provider.QueryResult{}, p.queryFailure
	}
	if p.queryWaiting {
		return provider.QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: "existing-unpaid", State: "WAIT_BUYER_PAY", Amount: 125, Currency: "CNY"}, nil
	}
	return p.releaseFlowProvider.Query(ctx, r)
}

func (p *manualCaptureProvider) CaptureAuthorization(_ context.Context, r provider.AuthorizationCaptureRequest) (provider.PaymentEvent, error) {
	p.writes.Add(1)
	if p.captureFailure != nil {
		return provider.PaymentEvent{}, p.captureFailure
	}
	return provider.PaymentEvent{EventID: "manual-capture-payment", TradeNo: r.TradeNo, ProviderTradeNo: "manual-capture-payment", BuyerID: r.BuyerID, Amount: r.Amount, State: "SUCCESS", Currency: "CNY"}, nil
}
func (p *manualCaptureProvider) QueryAuthorization(ctx context.Context, r provider.AuthorizationRequest) (provider.AuthorizationEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.beforeAuthorization != nil {
		p.beforeAuthorization()
	}
	event, e := p.releaseFlowProvider.QueryAuthorization(ctx, r)
	if p.wrongFreeze {
		event.OperationType = "UNFREEZE"
	}
	return event, e
}
func TestManualPreauthorizationCaptureConcurrentSingleSendAndReceipt(t *testing.T) {
	s, o := releaseTestOrder(t)
	ctx := context.Background()
	p := &manualCaptureProvider{}
	p.remainingKnown = true
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); g.CapturePreauthorization(ctx, o.MerchantID, o.TradeNo) }()
	}
	wg.Wait()
	a, e := s.Preauthorization(ctx, o.TradeNo)
	bal, _ := s.Balance(ctx, o.MerchantID)
	if e != nil || p.writes.Load() != 1 || a.CaptureState != "succeeded" || bal.Available != 125 {
		t.Fatal(a, bal, p.writes.Load(), e)
	}
	if _, e = g.CapturePreauthorization(ctx, o.MerchantID, o.TradeNo); e == nil || p.writes.Load() != 1 {
		t.Fatal("manual success could be retried", e)
	}
}
func TestManualPreauthorizationCaptureUnknownAndFailedNeverResend(t *testing.T) {
	for _, failure := range []error{&provider.UnknownOutcome{Operation: "capture", Cause: errors.New("lost response")}, &provider.ProviderError{Code: "40004:ACQ.PAYMENT_AUTH_CODE_INVALID"}} {
		t.Run(failure.Error(), func(t *testing.T) {
			s, o := releaseTestOrder(t)
			ctx := context.Background()
			p := &manualCaptureProvider{captureFailure: failure}
			p.remainingKnown = true
			g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
			for i := 0; i < 3; i++ {
				g.CapturePreauthorization(ctx, o.MerchantID, o.TradeNo)
			}
			a, e := s.Preauthorization(ctx, o.TradeNo)
			b, _ := s.Balance(ctx, o.MerchantID)
			if e != nil || p.writes.Load() != 1 || a.CaptureState == "none" || b.Available != 0 {
				t.Fatal(a, b, p.writes.Load(), e)
			}
		})
	}
}
func TestManualPreauthorizationCaptureRequiresFreshFullFreezeAndReleaseExclusion(t *testing.T) {
	for _, kind := range []string{"missing-rest", "partial", "wrong-operation", "release-wins", "stop", "foreign-merchant", "disabled", "query-auth-error", "query-network-error", "different-notfound", "query-waiting"} {
		t.Run(kind, func(t *testing.T) {
			s, o := releaseTestOrder(t)
			ctx := context.Background()
			p := &manualCaptureProvider{}
			p.remainingKnown = true
			g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
			mid := o.MerchantID
			switch kind {
			case "missing-rest":
				p.remainingKnown = false
			case "partial":
				p.partialRemaining = true
			case "wrong-operation":
				p.wrongFreeze = true
			case "query-auth-error":
				p.queryFailure = provider.ErrAuthentication
			case "query-network-error":
				p.queryFailure = errors.New("local unavailable")
			case "different-notfound":
				p.queryFailure = &provider.ProviderError{Code: "40004:AUTH.NOT_EXIST"}
			case "query-waiting":
				p.queryWaiting = true
			case "release-wins":
				p.beforeAuthorization = func() {
					if _, _, e := s.BeginAuthorizationRelease(ctx, o.MerchantID, o.TradeNo); e != nil {
						t.Error(e)
					}
				}
			case "stop":
				g.AllowLive = false
			case "foreign-merchant":
				mid++
			case "disabled":
				if _, e := s.pool.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=$1", mid); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := g.CapturePreauthorization(ctx, mid, o.TradeNo); e == nil || p.writes.Load() != 0 {
				t.Fatal("unsafe manual capture", kind, p.writes.Load(), e)
			}
		})
	}
}

func TestManualPreauthorizationCaptureRacesReleaseWithoutDoubleWrite(t *testing.T) {
	s, o := releaseTestOrder(t)
	ctx := context.Background()
	p := &manualCaptureProvider{}
	p.remainingKnown = true
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; g.CapturePreauthorization(ctx, o.MerchantID, o.TradeNo) }()
	go func() { defer wg.Done(); <-start; g.ReleaseAuthorization(ctx, o.MerchantID, o.TradeNo) }()
	close(start)
	wg.Wait()
	if p.writes.Load()+int32(p.unfreezes) > 1 {
		t.Fatal("capture and release both sent", p.writes.Load(), p.unfreezes)
	}
	if _, e := g.CapturePreauthorization(ctx, o.MerchantID, o.TradeNo); e == nil {
		t.Fatal("race result could reacquire capture")
	}
}

func TestManualPreauthorizationCapturePermissionLostAfterClaimNeverSendsOrRearms(t *testing.T) {
	s, o := releaseTestOrder(t)
	ctx := context.Background()
	p := &manualCaptureProvider{}
	p.remainingKnown = true
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	checks := 0
	guard := func(context.Context) error {
		checks++
		if checks == 3 {
			return ErrLiveDisabled
		}
		return nil
	}
	a, e := g.CapturePreauthorizationChecked(ctx, o.MerchantID, o.TradeNo, guard)
	if e == nil || a.CaptureState != "failed" || p.writes.Load() != 0 || checks != 3 {
		t.Fatal("claimed request sent after permission loss", a, e, p.writes.Load(), checks)
	}
	if _, e = g.CapturePreauthorization(ctx, o.MerchantID, o.TradeNo); e == nil || p.writes.Load() != 0 {
		t.Fatal("unsent but claimed request rearmed", e)
	}
}
