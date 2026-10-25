package payment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func settlementMerchant(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	for _, id := range []int64{681, 682} {
		if e := s.CreateMerchant(ctx, Merchant{ID: id, Name: "settlement minimum", APIKey: "test-only"}); e != nil {
			t.Fatal(e)
		}
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 681, OutTradeNo: "minimum-funding", Amount: 10000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "minimum-funding"); e != nil {
		t.Fatal(e)
	}
}

func TestSettlementMinimumBeforeReservationAndHistoricalReplay(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	// Missing policy preserves the existing zero-minimum behavior.
	legacy, e := s.RequestFunds(ctx, "settlements", 681, 0, "legacy", 1, "merchant:681")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReviewFundsRequest(ctx, legacy.ID, false, "admin:1"); e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "settlement_minimum_cents", "101")
	for _, actor := range []string{"merchant:681", "admin:1"} {
		if _, e = s.RequestFunds(ctx, "settlements", 681, 0, actor, 100, actor); !errors.Is(e, ErrInvalid) {
			t.Fatalf("%s bypassed minimum before reservation: %v", actor, e)
		}
	}
	b, _ := s.Balance(ctx, 681)
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_requests WHERE status='pending'").Scan(&count); e != nil || count != 0 || b.Available != 10000 || b.Reserved != 0 {
		t.Fatalf("rejected request changed funds/history: count=%d balance=%+v error=%v", count, b, e)
	}
	exact, e := s.RequestFunds(ctx, "settlements", 681, 0, "exact", 101, "admin:1")
	if e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "settlement_minimum_cents", "202")
	replay, e := s.RequestFunds(ctx, "settlements", 681, 0, "exact", 101, "merchant:681")
	if e != nil || replay.ID != exact.ID {
		t.Fatalf("policy change invalidated historical request: %+v %v", replay, e)
	}
	if _, e = s.RequestFunds(ctx, "settlements", 681, 0, "exact", 202, "admin:1"); !errors.Is(e, ErrConflict) {
		t.Fatal("replay changed immutable amount", e)
	}
	for _, kind := range []string{"internal-transfers", "profitsharing"} {
		if _, e = s.RequestFunds(ctx, kind, 681, 682, kind, 1, "merchant:681"); e != nil {
			t.Fatalf("settlement policy leaked into %s: %v", kind, e)
		}
	}
}

func TestSettlementMinimumRejectsCorruptPolicyWithoutFreezing(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	for i, raw := range []string{"", "1.01", "-1", "100000000001", "9223372036854775808", "invalid"} {
		transferSetting(t, s, "settlement_minimum_cents", raw)
		if _, e := s.RequestFunds(ctx, "settlements", 681, 0, fmt.Sprint(i), 100, "admin:1"); !errors.Is(e, ErrInvalid) {
			t.Fatalf("malformed cents %q accepted: %v", raw, e)
		}
	}
	b, _ := s.Balance(ctx, 681)
	if b.Available != 10000 || b.Reserved != 0 {
		t.Fatal("invalid policy froze funds", b)
	}
	transferSetting(t, s, "settlement_minimum_cents", "0")
	if _, e := s.RequestFunds(ctx, "settlements", 681, 0, "zero", 1, "merchant:681"); e != nil {
		t.Fatal(e)
	}
}

func TestSettlementMinimumConcurrentIdempotentReservation(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	transferSetting(t, s, "settlement_minimum_cents", "101")
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, e := s.RequestFunds(ctx, "settlements", 681, 0, "concurrent", 101, "admin:1"); e != nil {
				t.Error(e)
			}
			if _, e := s.RequestFunds(ctx, "settlements", 681, 0, fmt.Sprintf("below-%d", i), 100, "merchant:681"); !errors.Is(e, ErrInvalid) {
				t.Errorf("below minimum: %v", e)
			}
		}(i)
	}
	wg.Wait()
	b, _ := s.Balance(ctx, 681)
	var count int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_requests").Scan(&count); e != nil || count != 1 || b.Available != 9899 || b.Reserved != 101 {
		t.Fatalf("count=%d funds=%+v error=%v", count, b, e)
	}
}

func TestSettlementMinimumDoesNotBypassT1Availability(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	// Existing funding is prior-day income; this additional receipt is not yet
	// available. Meeting the minimum must not allow spending the receipt.
	if _, e := s.pool.Exec(ctx, "UPDATE orders SET paid_at=$1 WHERE merchant_id=681", time.Now().Add(-48*time.Hour)); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 681, OutTradeNo: "minimum-today", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "minimum-today"); e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "settle_type", "1")
	transferSetting(t, s, "settlement_minimum_cents", "101")
	if _, e = s.RequestFunds(ctx, "settlements", 681, 0, "today-not-mature", 10001, "admin:1"); !errors.Is(e, ErrInsufficient) {
		t.Fatalf("minimum check bypassed T+1: %v", e)
	}
	v, e := s.RequestFunds(ctx, "settlements", 681, 0, "mature", 10000, "merchant:681")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReviewFundsRequest(ctx, v.ID, false, "admin:1"); e != nil {
		t.Fatal(e)
	}
	b, _ := s.Balance(ctx, 681)
	if b.Available != 11000 || b.Reserved != 0 {
		t.Fatal("rejection did not restore exact reservation", b)
	}
}
