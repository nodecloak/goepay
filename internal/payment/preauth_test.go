package payment

import (
	"context"
	"errors"
	provider "goepay/internal/adapters/payment"
	"net/http"
	"sync"
	"testing"
)

func TestPreauthAuthorizationIsNotPaymentAndCaptureHasSingleOwner(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	a, _, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "preauth")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "pending", "preauth", "test-only-signed-sdk", ""); e != nil {
		t.Fatal(e)
	}
	event := provider.AuthorizationEvent{TradeNo: o.TradeNo, RequestNo: o.TradeNo, AuthNo: "authorization-42", BuyerID: "2088000000000000", Amount: 125, RestAmount: 125, State: "SUCCESS", OrderStatus: "AUTHORIZED"}
	if e = s.RecordPreauthorization(ctx, 7, event); e != nil {
		t.Fatal(e)
	}
	b, e := s.Balance(ctx, 70)
	if e != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatal("authorization moved merchant money", b, e)
	}
	got, _ := s.OrderByTradeNo(ctx, o.TradeNo)
	if got.Status == "paid" {
		t.Fatal("authorization marked paid")
	}
	for _, alter := range []provider.AuthorizationEvent{{TradeNo: o.TradeNo, RequestNo: o.TradeNo, AuthNo: "different-auth", BuyerID: event.BuyerID, Amount: 125, State: "SUCCESS"}, {TradeNo: o.TradeNo, RequestNo: o.TradeNo, AuthNo: event.AuthNo, BuyerID: "different-buyer", Amount: 125, State: "SUCCESS"}} {
		if e = s.RecordPreauthorization(ctx, 7, alter); !errors.Is(e, ErrConflict) {
			t.Fatal("authorization identity changed", e)
		}
	}
	var wg sync.WaitGroup
	owners := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, owner, e := s.BeginPreauthorizationCapture(ctx, o.TradeNo)
			if e != nil {
				t.Error(e)
			}
			owners <- owner
		}()
	}
	wg.Wait()
	close(owners)
	count := 0
	for owner := range owners {
		if owner {
			count++
		}
	}
	if count != 1 {
		t.Fatal("capture could be sent repeatedly", count)
	}
	if e = s.RecordPreauthorizationCaptureState(ctx, o.TradeNo, "unknown"); e != nil {
		t.Fatal(e)
	}
	current, owner, e := s.BeginPreauthorizationCapture(ctx, o.TradeNo)
	if e != nil || owner || current.CaptureState != "unknown" {
		t.Fatal("unknown capture reacquired", current, owner, e)
	}
	var jobs int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE event_key=$1", "preauth-capture-reconcile:"+o.TradeNo).Scan(&jobs); e != nil || jobs != 1 {
		t.Fatal("capture recovery not durable", jobs, e)
	}
}

type preauthFlowProvider struct {
	flowProvider
	captureErr      error
	captures        int
	buyer           string
	queryPaid       bool
	closes          int
	queryAuthorized bool
}

func (p *preauthFlowProvider) ID() string { return "alipay" }
func (p *preauthFlowProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Methods: []string{"preauth"}, Query: true, Close: true}
}
func (p *preauthFlowProvider) Close(context.Context, provider.QueryRequest) error {
	p.closes++
	return nil
}
func (p *preauthFlowProvider) Query(_ context.Context, r provider.QueryRequest) (provider.QueryResult, error) {
	state := "WAIT_BUYER_PAY"
	if p.queryPaid {
		state = "TRADE_SUCCESS"
	}
	return provider.QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: "captured-payment", Amount: 125, State: state, Currency: "CNY"}, nil
}
func (p *preauthFlowProvider) Freeze(context.Context, provider.CreateRequest) (provider.CreateResult, error) {
	return provider.CreateResult{MaterialType: "preauth", Material: "test-only-signed-sdk"}, nil
}
func (p *preauthFlowProvider) VerifyAuthorization(_ context.Context, _ http.Header, b []byte, e provider.ExpectedPayment) (provider.AuthorizationEvent, error) {
	if string(b) != "valid-authorization" {
		return provider.AuthorizationEvent{}, provider.ErrAuthentication
	}
	return provider.AuthorizationEvent{TradeNo: e.TradeNo, RequestNo: e.TradeNo, AuthNo: "authorization-42", BuyerID: "2088000000000000", Amount: e.Amount, RestAmount: e.Amount, State: "SUCCESS", OrderStatus: "AUTHORIZED"}, nil
}
func (p *preauthFlowProvider) CaptureAuthorization(_ context.Context, r provider.AuthorizationCaptureRequest) (provider.PaymentEvent, error) {
	p.captures++
	if p.captureErr != nil {
		return provider.PaymentEvent{}, p.captureErr
	}
	return provider.PaymentEvent{EventID: "captured-payment", TradeNo: r.TradeNo, ProviderTradeNo: "captured-payment", BuyerID: r.BuyerID, Amount: r.Amount, State: "SUCCESS", Currency: "CNY"}, nil
}
func (p *preauthFlowProvider) QueryAuthorization(_ context.Context, r provider.AuthorizationRequest) (provider.AuthorizationEvent, error) {
	if !p.queryAuthorized {
		return provider.AuthorizationEvent{}, provider.ErrUnsupported
	}
	return provider.AuthorizationEvent{TradeNo: r.TradeNo, RequestNo: r.RequestNo, AuthNo: "authorization-42", BuyerID: "2088000000000000", Amount: r.Amount, RestAmount: r.Amount, State: "SUCCESS", OrderStatus: "AUTHORIZED"}, nil
}
func (p *preauthFlowProvider) UnfreezeAuthorization(context.Context, provider.AuthorizationRequest) (provider.AuthorizationEvent, error) {
	return provider.AuthorizationEvent{}, provider.ErrUnsupported
}
func (p *preauthFlowProvider) CancelAuthorization(context.Context, provider.AuthorizationRequest) (provider.AuthorizationEvent, error) {
	return provider.AuthorizationEvent{}, provider.ErrUnsupported
}

func TestPreauthFlowPersistsAuthorizationDuringStopAndNeverResendsUnknownCapture(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	p := &preauthFlowProvider{captureErr: &provider.UnknownOutcome{Operation: "capture", Cause: errors.New("connection lost")}}
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	if _, e := g.SubmitPreauthorization(ctx, o.TradeNo, 7, "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	g.AllowLive = false
	if e := g.HandlePreauthorizationNotification(ctx, 7, o.TradeNo, nil, []byte("valid-authorization")); e != nil {
		t.Fatal(e)
	}
	a, e := s.Preauthorization(ctx, o.TradeNo)
	if e != nil || a.CaptureState != "none" || p.captures != 0 {
		t.Fatal("stop switch lost authorization or captured", a, e)
	}
	g.AllowLive = true
	for i := 0; i < 3; i++ {
		if e := g.HandlePreauthorizationNotification(ctx, 7, o.TradeNo, nil, []byte("valid-authorization")); e != nil {
			t.Fatal(e)
		}
	}
	a, _ = s.Preauthorization(ctx, o.TradeNo)
	b, _ := s.Balance(ctx, 70)
	if a.CaptureState != "unknown" || p.captures != 1 || b.Available != 0 {
		t.Fatal("unknown capture resent/credited", a, p.captures, b)
	}
}
func TestPreauthFlowCaptureSuccessUsesPaymentReceiptAndCreditsOnce(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	p := &preauthFlowProvider{}
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	if _, e := g.SubmitPreauthorization(ctx, o.TradeNo, 7, "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if e := g.HandlePreauthorizationNotification(ctx, 7, o.TradeNo, nil, []byte("valid-authorization")); e != nil {
			t.Fatal(e)
		}
	}
	b, _ := s.Balance(ctx, 70)
	a, _ := s.Preauthorization(ctx, o.TradeNo)
	var trade string
	e := s.pool.QueryRow(ctx, "SELECT provider_trade_no FROM verified_payment_receipts WHERE order_id=$1", o.ID).Scan(&trade)
	if e != nil || b.Available != 125 || a.CaptureState != "succeeded" || trade != "captured-payment" || p.captures != 1 {
		t.Fatal(b, a, trade, p.captures, e)
	}
}

func TestPreauthUnknownCaptureRecoversOnlyThroughReadOnlyQuery(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	p := &preauthFlowProvider{captureErr: &provider.UnknownOutcome{Operation: "capture", Cause: errors.New("disconnected")}}
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	if _, e := g.SubmitPreauthorization(ctx, o.TradeNo, 7, "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	if e := g.HandlePreauthorizationNotification(ctx, 7, o.TradeNo, nil, []byte("valid-authorization")); e != nil {
		t.Fatal(e)
	}
	g.AllowLive = false
	p.queryPaid = true
	if _, e := g.Query(ctx, 70, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	a, e := s.Preauthorization(ctx, o.TradeNo)
	b, _ := s.Balance(ctx, 70)
	if e != nil || a.CaptureState != "succeeded" || b.Available != 125 || p.captures != 1 {
		t.Fatal("query did not finish original capture", a, b, p.captures, e)
	}
}

func TestPreauthOrdinaryCloseCannotPretendToReleaseBuyerAuthorization(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	p := &preauthFlowProvider{}
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	if _, e := g.SubmitPreauthorization(ctx, o.TradeNo, 7, "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	g.AllowLive = false
	if e := g.HandlePreauthorizationNotification(ctx, 7, o.TradeNo, nil, []byte("valid-authorization")); e != nil {
		t.Fatal(e)
	}
	g.AllowLive = true
	if e := g.Close(ctx, 70, o.TradeNo); !errors.Is(e, provider.ErrUnsupported) {
		t.Fatal("ordinary close reported authorization release", e)
	}
	current, _ := s.OrderByTradeNo(ctx, o.TradeNo)
	if current.Status == "closed" || p.closes != 0 {
		t.Fatal("ordinary close reached provider or changed order", current.Status, p.closes)
	}
}

func TestPreauthReadOnlyQueryRecoversMissingAuthorizationNotificationWithoutCapture(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	p := &preauthFlowProvider{queryAuthorized: true}
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	if _, e := g.SubmitPreauthorization(ctx, o.TradeNo, 7, "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	g.AllowLive = false
	if _, e := g.Query(ctx, 70, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	a, e := s.Preauthorization(ctx, o.TradeNo)
	b, _ := s.Balance(ctx, 70)
	if e != nil || a.AuthNo != "authorization-42" || a.CaptureState != "none" || b.Available != 0 || p.captures != 0 {
		t.Fatal("missing notify was not recovered read-only", a, b, p.captures, e)
	}
}
