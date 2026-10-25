package payment

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

func BenchmarkPGOrderCreate(b *testing.B) {
	s := testStore(b)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 1, Name: "Benchmark isolated", APIKey: "local-test-benchmark-key"}); e != nil {
		b.Fatal(e)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1, OutTradeNo: fmt.Sprint(i), Name: "Test", Type: "sandbox", Amount: 100}); e != nil {
			b.Fatal(e)
		}
	}
}
func BenchmarkPGOrderQuery(b *testing.B) {
	s := testStore(b)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 1, Name: "Benchmark isolated", APIKey: "local-test-benchmark-key"}); e != nil {
		b.Fatal(e)
	}
	if _, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1, OutTradeNo: "benchmark", Name: "Test", Type: "sandbox", Amount: 100}); e != nil {
		b.Fatal(e)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, e := s.Order(ctx, 1, "benchmark"); e != nil {
			b.Fatal(e)
		}
	}
}

// This measures only concurrent PostgreSQL payment posting, including account
// contention. It excludes TLS/signature work and is not a provider throughput
// measurement or a comparison against the original PHP deployment.
func BenchmarkPGConcurrentPaid(b *testing.B) {
	s := testStore(b)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 1, Name: "Benchmark posting isolated", APIKey: "local-test-benchmark-key"}); e != nil {
		b.Fatal(e)
	}
	trades := make([]string, b.N)
	for i := range trades {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1, OutTradeNo: fmt.Sprintf("bench-paid-%d", i), Name: "local database benchmark", Type: "sandbox", Amount: 100})
		if e != nil {
			b.Fatal(e)
		}
		trades[i] = o.TradeNo
	}
	var sequence atomic.Int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := int(sequence.Add(1) - 1)
			if e := s.MarkPaid(ctx, trades[i], fmt.Sprintf("bench-paid-event-%d", i)); e != nil {
				b.Error(e)
				return
			}
		}
	})
	b.StopTimer()
	if b.Failed() {
		return
	}
	balance, e := s.Balance(ctx, 1)
	if e != nil || balance.Available != int64(b.N)*100 || balance.Reserved != 0 {
		b.Fatal("benchmark lost or duplicated payment posting", balance, e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		b.Fatal("benchmark violated ledger invariants", report, e)
	}
}
