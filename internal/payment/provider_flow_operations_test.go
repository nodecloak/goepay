package payment

import (
	"context"
	"errors"
	provider "goepay/internal/adapters/payment"
	"sync"
	"sync/atomic"
	"testing"
)

type operationsProvider struct {
	flowProvider
	refunds, closes atomic.Int32
	unknown         bool
}

func (p *operationsProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Methods: []string{"native"}, Query: true, Close: true, Refund: true, RefundQuery: true}
}
func (p *operationsProvider) Query(_ context.Context, r provider.QueryRequest) (provider.QueryResult, error) {
	return provider.QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: "upstream-paid", State: "SUCCESS", Amount: 100, Currency: "CNY"}, nil
}
func (p *operationsProvider) Close(context.Context, provider.QueryRequest) error {
	p.closes.Add(1)
	return nil
}
func (p *operationsProvider) Refund(_ context.Context, r provider.RefundRequest) (provider.RefundResult, error) {
	p.refunds.Add(1)
	if p.unknown {
		return provider.RefundResult{}, &provider.UnknownOutcome{Operation: "refund", Cause: errors.New("test lost response")}
	}
	return provider.RefundResult{RefundNo: r.RefundNo, Amount: r.Amount, State: "PROCESSING"}, nil
}
func (p *operationsProvider) QueryRefund(_ context.Context, r provider.RefundRequest) (provider.RefundResult, error) {
	return provider.RefundResult{RefundNo: r.RefundNo, Amount: r.Amount, State: "SUCCESS"}, nil
}
func setupOperations(t *testing.T) (*Store, ProviderFlow, *operationsProvider, Order) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 1, Name: "test", APIKey: "test-only-secret"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1, OutTradeNo: "provider-ops", Name: "test", Type: "wxpay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	p := &operationsProvider{}
	flow := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://test.example.invalid"}
	if _, e := flow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	return s, flow, p, o
}
func TestProviderRefundUnknownReservedAndNoResubmit(t *testing.T) {
	s, flow, p, o := setupOperations(t)
	ctx := context.Background()
	if _, e := flow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	p.unknown = true
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); flow.Refund(ctx, 1, o.TradeNo, "refund1", 60) }()
	}
	wg.Wait()
	if p.refunds.Load() != 1 {
		t.Fatalf("duplicate external refunds %d", p.refunds.Load())
	}
	b, _ := s.Balance(ctx, 1)
	if b.Available != 40 || b.Reserved != 60 {
		t.Fatalf("unknown released funds %v", b)
	}
	r, e := flow.QueryRefund(ctx, 1, "refund1")
	if e != nil || r.Status != "succeeded" {
		t.Fatalf("reconcile refund %v %v", r, e)
	}
	b, _ = s.Balance(ctx, 1)
	if b.Available != 40 || b.Reserved != 0 {
		t.Fatal(b)
	}
}
func TestProviderCloseAndLatePaidQuery(t *testing.T) {
	s, flow, p, o := setupOperations(t)
	ctx := context.Background()
	if e := flow.Close(ctx, 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	if e := flow.Close(ctx, 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	if p.closes.Load() != 1 {
		t.Fatal("close resubmitted")
	}
	if _, e := flow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal("verified late payment discarded", e)
	}
	order, _ := s.OrderByTradeNo(ctx, o.TradeNo)
	b, _ := s.Balance(ctx, 1)
	if order.Status != "paid" || b.Available != 100 {
		t.Fatalf("late paid %v %v", order, b)
	}
	var n int
	s.pool.QueryRow(ctx, "SELECT count(*) FROM provider_reconciliations").Scan(&n)
	if n != 1 {
		t.Fatal("missing late-paid reconciliation audit")
	}
}
func TestProviderRefundResultCommitsWithLedger(t *testing.T) {
	s, flow, p, o := setupOperations(t)
	ctx := context.Background()
	flow.Query(ctx, 1, o.TradeNo)
	p.unknown = true
	flow.Refund(ctx, 1, o.TradeNo, "refund-atomic", 60)
	_, e := s.pool.Exec(ctx, `CREATE FUNCTION fail_refund_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected status failure'; END $$;CREATE TRIGGER fail_refund_test BEFORE UPDATE ON refunds FOR EACH ROW EXECUTE FUNCTION fail_refund_test()`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := flow.QueryRefund(ctx, 1, "refund-atomic"); e == nil {
		t.Fatal("injected failure ignored")
	}
	var state string
	s.pool.QueryRow(ctx, "SELECT state FROM provider_operations WHERE kind='refund'").Scan(&state)
	if state != "unknown" {
		t.Fatal("provider outcome committed without refund ledger", state)
	}
	b, _ := s.Balance(ctx, 1)
	if b.Reserved != 60 {
		t.Fatal("reservation lost")
	}
}

type namespacedRefundProvider struct {
	operationsProvider
	mu       sync.Mutex
	business map[string]string
}

func (p *namespacedRefundProvider) Refund(_ context.Context, r provider.RefundRequest) (provider.RefundResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if previous := p.business[r.RefundNo]; previous != "" && previous != r.TradeNo {
		return provider.RefundResult{}, errors.New("refund ID collided across merchants")
	}
	p.business[r.RefundNo] = r.TradeNo
	return provider.RefundResult{RefundNo: r.RefundNo, Amount: r.Amount, State: "PROCESSING"}, nil
}
func TestProviderRefundIDsAreNamespacedAcrossMerchants(t *testing.T) {
	s, flow, _, first := setupOperations(t)
	ctx := context.Background()
	p := &namespacedRefundProvider{business: map[string]string{}}
	flow.Providers[7] = p
	if e := s.MarkPaid(ctx, first.TradeNo, "merchant1-payment"); e != nil {
		t.Fatal(e)
	}
	if e := s.CreateMerchant(ctx, Merchant{ID: 2, Name: "Merchant2", APIKey: "test-only-secret2"}); e != nil {
		t.Fatal(e)
	}
	second, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 2, OutTradeNo: "same-client-order", Name: "test", Type: "wxpay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := flow.Submit(ctx, second.TradeNo, 7, "native", "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	if e := s.MarkPaid(ctx, second.TradeNo, "merchant2-payment"); e != nil {
		t.Fatal(e)
	}
	if _, e := flow.Refund(ctx, 1, first.TradeNo, "refund-1", 50); e != nil {
		t.Fatal(e)
	}
	if _, e := flow.Refund(ctx, 2, second.TradeNo, "refund-1", 50); e != nil {
		t.Fatal(e)
	}
	if len(p.business) != 2 {
		t.Fatal("different merchants shared upstream refund number")
	}
	for number := range p.business {
		if number == "refund-1" {
			t.Fatal("merchant-controlled ID exposed as upstream namespace")
		}
	}
}
