package payment

import (
	"context"
	"errors"
	provider "goepay/internal/adapters/payment"
	"testing"
)

type subjectFlowProvider struct {
	flowProvider
	t *testing.T
}

func (p *subjectFlowProvider) Create(ctx context.Context, r provider.CreateRequest) (provider.CreateResult, error) {
	if r.Subject != "upstream-only" {
		p.t.Errorf("ordinary raw subject %q", r.Subject)
	}
	return p.flowProvider.Create(ctx, r)
}

type subjectScanProvider struct {
	scanFlowProvider
	t *testing.T
}

func (p *subjectScanProvider) Scan(ctx context.Context, r provider.ScanRequest) (provider.PaymentEvent, error) {
	if r.Subject != "upstream-only" {
		p.t.Errorf("scan raw subject %q", r.Subject)
	}
	return p.scanFlowProvider.Scan(ctx, r)
}

type subjectPreauthProvider struct {
	preauthFlowProvider
	t *testing.T
}

func (p *subjectPreauthProvider) Freeze(ctx context.Context, r provider.CreateRequest) (provider.CreateResult, error) {
	if r.Subject != "upstream-only" {
		p.t.Errorf("freeze raw subject %q", r.Subject)
	}
	return p.preauthFlowProvider.Freeze(ctx, r)
}
func (p *subjectPreauthProvider) CaptureAuthorization(ctx context.Context, r provider.AuthorizationCaptureRequest) (provider.PaymentEvent, error) {
	if r.Subject != "upstream-only" {
		p.t.Errorf("capture raw subject %q", r.Subject)
	}
	return p.preauthFlowProvider.CaptureAuthorization(ctx, r)
}

func TestProviderSubjectHookAllFourSendsAndRejectBeforeAttempt(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	if e := s.CreateMerchant(ctx, Merchant{ID: 1, Name: "test", APIKey: "test-only"}); e != nil {
		t.Fatal(e)
	}
	resolve := func(_ context.Context, o Order, ch int64) (string, error) {
		if o.Name != "original" || ch != 7 {
			t.Error(o.Name, ch)
		}
		return "upstream-only", nil
	}
	create := func(no, typ string, amount int64) Order {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1, OutTradeNo: no, Name: "original", Type: typ, Amount: amount})
		if e != nil {
			t.Fatal(e)
		}
		return o
	}
	p := &subjectFlowProvider{t: t}
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid", SubjectResolver: resolve}
	ordinary := create("ordinary", "wxpay", 100)
	if _, e := g.Submit(ctx, ordinary.TradeNo, 7, "native", "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	scanner := &subjectScanProvider{t: t}
	g.Providers[7] = scanner
	scan := create("scan", "wxpay", 100)
	if _, e := g.SubmitScan(ctx, scan.TradeNo, 7, "192.0.2.1", "134567890123456789"); e != nil {
		t.Fatal(e)
	}
	preauth := &subjectPreauthProvider{t: t}
	g.Providers[7] = preauth
	auth := create("auth", "alipay", 125)
	if _, e := g.SubmitPreauthorization(ctx, auth.TradeNo, 7, "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	if e := g.HandlePreauthorizationNotification(ctx, 7, auth.TradeNo, nil, []byte("valid-authorization")); e != nil {
		t.Fatal(e)
	}
	if preauth.captures != 1 {
		t.Fatal("capture not exercised")
	}
	g.Providers[7] = p
	bad := create("invalid", "wxpay", 100)
	g.SubjectResolver = func(context.Context, Order, int64) (string, error) { return "", ErrInvalid }
	if _, e := g.Submit(ctx, bad.TradeNo, 7, "native", "192.0.2.1", ""); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if _, e := s.ProviderAttempt(ctx, bad.TradeNo); !errors.Is(e, ErrNotFound) {
		t.Fatal("invalid title acquired send intent", e)
	}
	if p.calls.Load() != 1 {
		t.Fatal("invalid title sent upstream")
	}
	for _, o := range []Order{ordinary, scan, auth, bad} {
		stored, e := s.OrderByTradeNo(ctx, o.TradeNo)
		if e != nil || stored.Name != "original" {
			t.Fatal("merchant title changed", e)
		}
	}
}
