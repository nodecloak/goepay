package payment

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type scanFlowProvider struct {
	flowProvider
	scans      atomic.Int32
	queryState string
	lost       bool
}

func (*scanFlowProvider) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Methods: []string{"scan"}, Query: true}
}
func (*scanFlowProvider) ValidateScan(r adapter.ScanRequest) error {
	kind, e := adapter.ScanPaymentType(r.AuthCode)
	if e != nil || kind != "wxpay" {
		return adapter.ErrInvalid
	}
	return nil
}
func (p *scanFlowProvider) Scan(_ context.Context, r adapter.ScanRequest) (adapter.PaymentEvent, error) {
	p.scans.Add(1)
	if p.lost {
		return adapter.PaymentEvent{}, &adapter.UnknownOutcome{Operation: "scan", Cause: errors.New("synthetic timeout")}
	}
	return adapter.PaymentEvent{TradeNo: r.TradeNo, EventID: "upstream-" + r.TradeNo, ProviderTradeNo: "upstream-" + r.TradeNo, Amount: r.Amount, Currency: "CNY", State: "SUCCESS", BuyerID: "synthetic-buyer"}, nil
}
func (p *scanFlowProvider) Query(_ context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	return adapter.QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: "upstream-" + r.TradeNo, Amount: 100, Currency: "CNY", State: p.queryState, BuyerID: "synthetic-buyer"}, nil
}

func TestScanFlowSingleSendUnknownQueryAndLateCallback(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if e := store.CreateMerchant(ctx, Merchant{ID: 1, Name: "test", APIKey: "synthetic-secret"}); e != nil {
		t.Fatal(e)
	}
	p := &scanFlowProvider{queryState: "USERPAYING"}
	g := ProviderFlow{Store: store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	scanner, ok := any(g).(interface {
		SubmitScan(context.Context, string, int64, string, string) (Order, error)
	})
	if !ok {
		t.Fatal("durable scan flow missing")
	}
	create := func(no string) Order {
		o, e := store.CreateOrder(ctx, CreateOrderInput{MerchantID: 1, OutTradeNo: no, Name: "test", Type: "wxpay", Amount: 100})
		if e != nil {
			t.Fatal(e)
		}
		return o
	}
	o := create("scan-concurrent")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); scanner.SubmitScan(ctx, o.TradeNo, 7, "192.0.2.1", "120000000000000001") }()
	}
	wg.Wait()
	current, e := store.OrderByTradeNo(ctx, o.TradeNo)
	b, _ := store.Balance(ctx, 1)
	if e != nil || current.Status != "paid" || b.Available != 100 || p.scans.Load() != 1 {
		t.Fatal("duplicate scan or false settlement", current.Status, b, p.scans.Load(), e)
	}
	if _, e = scanner.SubmitScan(ctx, o.TradeNo, 7, "192.0.2.1", "120000000000000002"); !errors.Is(e, ErrConflict) {
		t.Fatal("changed bearer code reused paid result", e)
	}
	p.lost = true
	o = create("scan-unknown")
	if _, e = scanner.SubmitScan(ctx, o.TradeNo, 7, "192.0.2.1", "120000000000000003"); e == nil {
		t.Fatal("unknown scan accepted")
	}
	if _, e = scanner.SubmitScan(ctx, o.TradeNo, 7, "192.0.2.1", "120000000000000003"); e == nil || p.scans.Load() != 2 {
		t.Fatal("unknown scan resubmitted", e, p.scans.Load())
	}
	g.AllowLive = false
	if _, e = g.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	b, _ = store.Balance(ctx, 1)
	if b.Available != 100 {
		t.Fatal("pending scan credited", b)
	}
	p.queryState = "SUCCESS"
	for i := 0; i < 3; i++ {
		if _, e = g.Query(ctx, 1, o.TradeNo); e != nil {
			t.Fatal(e)
		}
	}
	if e = g.HandleNotification(ctx, 7, o.TradeNo, nil, []byte("valid-signed-test-event")); e != nil {
		t.Fatal(e)
	}
	b, _ = store.Balance(ctx, 1)
	if b.Available != 200 || p.scans.Load() != 2 {
		t.Fatal("late scan callback double credited", b, p.scans.Load())
	}
	var buyer, raw string
	if e = store.pool.QueryRow(ctx, "SELECT buyer FROM order_reporting WHERE order_id=$1", o.ID).Scan(&buyer); e != nil || buyer != "synthetic-buyer" {
		t.Fatal("verified query buyer lost", buyer, e)
	}
	if e = store.pool.QueryRow(ctx, "SELECT row_to_json(a)::text FROM provider_attempts a WHERE order_id=$1", o.ID).Scan(&raw); e != nil || strings.Contains(raw, "120000000000000003") {
		t.Fatal("bearer code persisted", e)
	}
	o = create("scan-invalid")
	if _, e = scanner.SubmitScan(ctx, o.TradeNo, 7, "192.0.2.1", "invalid"); e == nil {
		t.Fatal("invalid scan accepted")
	}
	if _, e = store.ProviderAttempt(ctx, o.TradeNo); !errors.Is(e, ErrNotFound) {
		t.Fatal("invalid scan created attempt", e)
	}
}
