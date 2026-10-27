package payment

import (
	"context"
	"errors"
	"fmt"
	provider "goepay/internal/adapters/payment"
	"sync"
	"testing"
)

func releaseTestOrder(t *testing.T) (*Store, Order) {
	t.Helper()
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	a, _, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "preauth")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "pending", "preauth", "test-only-sdk", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordPreauthorization(ctx, 7, provider.AuthorizationEvent{TradeNo: o.TradeNo, RequestNo: o.TradeNo, AuthNo: "release-auth", BuyerID: "2088000000000000", Amount: 125, State: "SUCCESS"}); e != nil {
		t.Fatal(e)
	}
	return s, o
}
func TestAuthorizationReleaseIntentBlocksCaptureAndClosesWithoutMerchantCredit(t *testing.T) {
	s, o := releaseTestOrder(t)
	ctx := context.Background()
	r, owner, e := s.BeginAuthorizationRelease(ctx, 70, o.TradeNo)
	if e != nil || !owner || r.RequestNo != fmt.Sprintf("UA%d", o.ID) {
		t.Fatal(r, owner, e)
	}
	if _, owner, e := s.BeginPreauthorizationCapture(ctx, o.TradeNo); e != nil || owner {
		t.Fatal("release allowed capture", owner, e)
	}
	if _, owner, e := s.BeginAuthorizationRelease(ctx, 70, o.TradeNo); e != nil || owner {
		t.Fatal("release intent repeated", owner, e)
	}
	if e = s.MarkAuthorizationReleaseSent(ctx, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveAuthorizationRelease(ctx, o.TradeNo, "succeeded"); e != nil {
		t.Fatal(e)
	}
	order, _ := s.OrderByTradeNo(ctx, o.TradeNo)
	balance, _ := s.Balance(ctx, 70)
	auth, _ := s.Preauthorization(ctx, o.TradeNo)
	if order.Status != "closed" || auth.ReleaseState != "succeeded" || balance.Available != 0 || balance.Reserved != 0 {
		t.Fatal("release invented a refund or failed to close", order.Status, auth, balance)
	}
	if e = s.markPaid(ctx, o.TradeNo, "late-real-payment", true, PaidReceipt{RealMoney: o.Amount, ChannelID: 7, ProviderTradeNo: "late-real-payment", BuyerID: auth.BuyerID}); e != nil {
		t.Fatal("release blocked late verified payment", e)
	}
	balance, _ = s.Balance(ctx, 70)
	if balance.Available != 125 {
		t.Fatal(balance)
	}
}

type releaseFlowProvider struct {
	preauthFlowProvider
	unfreezes        int
	releaseUnknown   bool
	queriedRequest   string
	released         bool
	remainingKnown   bool
	captureClosed    bool
	wrongOperation   bool
	partialRemaining bool
}

func (p *releaseFlowProvider) QueryAuthorization(_ context.Context, r provider.AuthorizationRequest) (provider.AuthorizationEvent, error) {
	p.queriedRequest = r.RequestNo
	operation := "FREEZE"
	rest := r.Amount
	if r.RequestNo != r.TradeNo {
		operation = "UNFREEZE"
		if !p.released {
			return provider.AuthorizationEvent{}, &provider.ProviderError{Code: "40004:AUTH.NOT_EXIST"}
		}
		rest = 0
	}
	if p.wrongOperation {
		operation = "FREEZE"
	}
	if p.partialRemaining && r.RequestNo == r.TradeNo {
		rest--
	}
	return provider.AuthorizationEvent{TradeNo: r.TradeNo, RequestNo: r.RequestNo, AuthNo: "release-auth", BuyerID: "2088000000000000", Amount: r.Amount, RestAmount: rest, RestAmountKnown: p.remainingKnown, OperationType: operation, State: "SUCCESS", OrderStatus: "AUTHORIZED"}, nil
}
func (p *releaseFlowProvider) Query(_ context.Context, r provider.QueryRequest) (provider.QueryResult, error) {
	if p.queryPaid {
		return provider.QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: "actual-payment", Amount: 125, State: "TRADE_SUCCESS", Currency: "CNY"}, nil
	}
	if p.captureClosed {
		return provider.QueryResult{TradeNo: r.TradeNo, Amount: 125, State: "TRADE_CLOSED", Currency: "CNY"}, nil
	}
	return provider.QueryResult{}, &provider.ProviderError{Code: "40004:ACQ.TRADE_NOT_EXIST"}
}
func (p *releaseFlowProvider) UnfreezeAuthorization(_ context.Context, r provider.AuthorizationRequest) (provider.AuthorizationEvent, error) {
	p.unfreezes++
	if r.RequestNo[:2] != "UA" {
		return provider.AuthorizationEvent{}, errors.New("unstable release number")
	}
	if p.releaseUnknown {
		return provider.AuthorizationEvent{}, &provider.UnknownOutcome{Operation: "unfreeze", Cause: errors.New("disconnected")}
	}
	p.released = true
	return provider.AuthorizationEvent{TradeNo: r.TradeNo, RequestNo: r.RequestNo, AuthNo: r.AuthNo, Amount: r.Amount, State: "SUCCESS"}, nil
}

func TestAuthorizationReleaseUnknownIsNeverResentAndQueryUsesOriginalUA(t *testing.T) {
	s, o := releaseTestOrder(t)
	p := &releaseFlowProvider{releaseUnknown: true, remainingKnown: true}
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true}
	ctx := context.Background()
	r, e := g.ReleaseAuthorization(ctx, 70, o.TradeNo)
	var unknown *provider.UnknownOutcome
	if !errors.As(e, &unknown) || r.State != "unknown" || p.unfreezes != 1 {
		t.Fatal(r, p.unfreezes, e)
	}
	_, _ = g.ReleaseAuthorization(ctx, 70, o.TradeNo)
	if p.unfreezes != 1 {
		t.Fatal("unknown release resent")
	}
	p.released = true
	g.AllowLive = false
	p.wrongOperation = true
	if _, e := g.QueryAuthorizationRelease(ctx, 70, o.TradeNo); e == nil {
		t.Fatal("wrong operation released authorization")
	}
	p.wrongOperation = false
	p.remainingKnown = false
	if _, e := g.QueryAuthorizationRelease(ctx, 70, o.TradeNo); e == nil {
		t.Fatal("missing remaining balance released authorization")
	}
	p.remainingKnown = true
	r, e = g.QueryAuthorizationRelease(ctx, 70, o.TradeNo)
	if e != nil || r.State != "succeeded" || p.queriedRequest != fmt.Sprintf("UA%d", o.ID) || p.unfreezes != 1 {
		t.Fatal(r, p.queriedRequest, p.unfreezes, e)
	}
	b, _ := s.Balance(ctx, 70)
	if b.Available != 0 {
		t.Fatal("buyer release credited merchant", b)
	}
}

func TestAuthorizationReleasePreflightFailureCanRetryOnlyFirstSendAndConcurrentClaim(t *testing.T) {
	s, o := releaseTestOrder(t)
	p := &releaseFlowProvider{remainingKnown: false}
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true}
	ctx := context.Background()
	r, _ := g.ReleaseAuthorization(ctx, 70, o.TradeNo)
	if r.State != "reversal_pending" || r.Sent || p.unfreezes != 0 {
		t.Fatal(r, p.unfreezes)
	}
	p.remainingKnown = true
	r, e := g.ReleaseAuthorization(ctx, 70, o.TradeNo)
	if e != nil || r.State != "succeeded" || p.unfreezes != 1 {
		t.Fatal(r, p.unfreezes, e)
	}
}
func TestAuthorizationReleaseSendBoundaryAndCaptureCannotBothWin(t *testing.T) {
	s, o := releaseTestOrder(t)
	ctx := context.Background()
	r, _, e := s.BeginAuthorizationRelease(ctx, 70, o.TradeNo)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.MarkAuthorizationReleaseSent(ctx, o.TradeNo) }()
	}
	wg.Wait()
	close(results)
	owners := 0
	for e := range results {
		if e == nil {
			owners++
		} else if !errors.Is(e, ErrState) {
			t.Fatal(e)
		}
	}
	if owners != 1 {
		t.Fatal("multiple first sends granted", owners)
	}
	if _, owner, e := s.BeginPreauthorizationCapture(ctx, o.TradeNo); e != nil || owner {
		t.Fatal("capture won after release send", owner, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE payment_authorizations SET capture_state='processing' WHERE order_id=$1", r.OrderID); e == nil {
		t.Fatal("database allowed capture after release intent")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE payment_authorization_releases SET request_no='replacement-UA' WHERE order_id=$1", r.OrderID); e == nil {
		t.Fatal("database allowed release request renumbering")
	}
}
func TestAuthorizationReleaseRequiresFullRemainingEvidenceAndResolvesUnknownCaptureFirst(t *testing.T) {
	for _, tt := range []struct {
		name                                 string
		known, paid, unknownCapture, partial bool
	}{{"missing-rest", false, false, false, false}, {"partial-rest", true, false, false, true}, {"capture-unknown-notfound", true, false, true, false}, {"capture-paid", true, true, true, false}} {
		t.Run(tt.name, func(t *testing.T) {
			s, o := releaseTestOrder(t)
			ctx := context.Background()
			if tt.unknownCapture {
				if _, _, e := s.BeginPreauthorizationCapture(ctx, o.TradeNo); e != nil {
					t.Fatal(e)
				}
				if e := s.RecordPreauthorizationCaptureState(ctx, o.TradeNo, "unknown"); e != nil {
					t.Fatal(e)
				}
			}
			p := &releaseFlowProvider{remainingKnown: tt.known, partialRemaining: tt.partial}
			p.queryPaid = tt.paid
			g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true}
			r, _ := g.ReleaseAuthorization(ctx, 70, o.TradeNo)
			if p.unfreezes != 0 {
				t.Fatal("unproved authorization released", r)
			}
			b, _ := s.Balance(ctx, 70)
			want := int64(0)
			if tt.paid {
				want = 125
			}
			if b.Available != want {
				t.Fatal(b, want)
			}
		})
	}
}

func TestAuthorizationReleaseCanSendFirstTimeAfterCaptureIsVerifiedClosed(t *testing.T) {
	s, o := releaseTestOrder(t)
	ctx := context.Background()
	if _, _, e := s.BeginPreauthorizationCapture(ctx, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordPreauthorizationCaptureState(ctx, o.TradeNo, "unknown"); e != nil {
		t.Fatal(e)
	}
	p := &releaseFlowProvider{remainingKnown: true}
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true}
	r, _ := g.ReleaseAuthorization(ctx, 70, o.TradeNo)
	if r.State != "reversal_pending" || r.Sent || p.unfreezes != 0 {
		t.Fatal(r, p.unfreezes)
	}
	p.captureClosed = true
	r, e := g.ReleaseAuthorization(ctx, 70, o.TradeNo)
	if e != nil || r.State != "succeeded" || p.unfreezes != 1 {
		t.Fatal("first unfreeze remained stuck after terminal capture evidence", r, p.unfreezes, e)
	}
}
