package payment

import (
	"context"
	"errors"
	provider "goepay/internal/adapters/payment"
	"sync"
	"testing"
)

type identityFlowProvider struct{ flowProvider }

func (f *identityFlowProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Methods: []string{"jsapi"}}
}
func (f *identityFlowProvider) ApplicationID() string { return "trusted-app" }

func TestConcurrentProviderIntentBindsPayerIdentity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 64001, Name: "payer intent", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 64001, OutTradeNo: "payer", Name: "test", Type: "wxpay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	p := &identityFlowProvider{}
	g := ProviderFlow{Store: s, Providers: map[int64]provider.Provider{8: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	if _, e = g.Submit(ctx, o.TradeNo, 8, "jsapi", "192.0.2.1", "first-buyer"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			buyer := "first-buyer"
			if i%2 == 1 {
				buyer = "changed-buyer"
			}
			_, e := g.Submit(ctx, o.TradeNo, 8, "jsapi", "192.0.2.1", buyer)
			if i%2 == 1 && !errors.Is(e, ErrConflict) {
				errs <- errors.New("changed payer reused an intent")
			} else if i%2 == 0 && e != nil {
				errs <- e
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if p.calls.Load() != 1 {
		t.Fatal("resubmitted", p.calls.Load())
	}
	var hash []byte
	if e = s.pool.QueryRow(ctx, "SELECT request_hash FROM provider_attempt_identities").Scan(&hash); e != nil || len(hash) != 32 {
		t.Fatal(hash, e)
	}
}
