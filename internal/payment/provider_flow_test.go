package payment

import (
	"context"
	"errors"
	provider "goepay/internal/adapters/payment"
	"net/http"
	"sync/atomic"
	"testing"
)

type flowProvider struct {
	calls   atomic.Int32
	unknown bool
}

func (f *flowProvider) ID() string { return "wxpay" }
func (f *flowProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Methods: []string{"native"}}
}
func (f *flowProvider) Create(_ context.Context, r provider.CreateRequest) (provider.CreateResult, error) {
	f.calls.Add(1)
	if f.unknown {
		return provider.CreateResult{}, &provider.UnknownOutcome{Operation: "create", Cause: errors.New("timeout")}
	}
	return provider.CreateResult{MaterialType: "qrcode", Material: "weixin://test-only"}, nil
}
func (f *flowProvider) Query(context.Context, provider.QueryRequest) (provider.QueryResult, error) {
	return provider.QueryResult{}, provider.ErrUnsupported
}
func (f *flowProvider) Close(context.Context, provider.QueryRequest) error {
	return provider.ErrUnsupported
}
func (f *flowProvider) Refund(context.Context, provider.RefundRequest) (provider.RefundResult, error) {
	return provider.RefundResult{}, provider.ErrUnsupported
}
func (f *flowProvider) QueryRefund(context.Context, provider.RefundRequest) (provider.RefundResult, error) {
	return provider.RefundResult{}, provider.ErrUnsupported
}
func (f *flowProvider) VerifyNotification(_ context.Context, _ http.Header, raw []byte, e provider.ExpectedPayment) (provider.PaymentEvent, error) {
	if string(raw) != "valid-signed-test-event" {
		return provider.PaymentEvent{}, provider.ErrAuthentication
	}
	return provider.PaymentEvent{EventID: "event1", TradeNo: e.TradeNo, ProviderTradeNo: "upstream-" + e.TradeNo, State: "SUCCESS", Currency: "CNY", Amount: e.Amount}, nil
}
func TestProviderFlowRetainsUnknownWithoutResubmission(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 1000, Name: "Test", APIKey: "test-only-secret-key"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1000, OutTradeNo: "flow-test", Name: "test", Type: "wxpay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	f := &flowProvider{unknown: true}
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{2: f}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	if _, e = g.Submit(ctx, o.TradeNo, 2, "native", "192.0.2.1", ""); e == nil {
		t.Fatal("unknown write accepted")
	}
	a, e := g.Submit(ctx, o.TradeNo, 2, "native", "192.0.2.1", "")
	if e != nil || a.State != "unknown" || f.calls.Load() != 1 {
		t.Fatalf("resubmitted unknown outcome: %v calls%d", e, f.calls.Load())
	}
	if e = g.HandleNotification(ctx, 2, o.TradeNo, nil, []byte("invalid")); e == nil {
		t.Fatal("unsigned callback accepted")
	}
	if e = g.HandleNotification(ctx, 2, o.TradeNo, nil, []byte("valid-signed-test-event")); e != nil {
		t.Fatal(e)
	}
	if e = g.HandleNotification(ctx, 2, o.TradeNo, nil, []byte("valid-signed-test-event")); e != nil {
		t.Fatal(e)
	}
	b, e := s.Balance(ctx, 1000)
	if e != nil || b.Available != 100 {
		t.Fatalf("credit %v %v", b, e)
	}
}
func TestProviderFlowLiveGate(t *testing.T) {
	f := &flowProvider{}
	g := ProviderFlow{Providers: map[int64]provider.Provider{2: f}}
	if _, e := g.Submit(context.Background(), "no-order", 2, "native", "", ""); !errors.Is(e, ErrLiveDisabled) {
		t.Fatalf("gate %v", e)
	}
	if f.calls.Load() != 0 {
		t.Fatal("called real provider while disabled")
	}
}
