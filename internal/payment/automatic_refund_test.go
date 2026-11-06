package payment

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestAutomaticAPIRefundConcurrentReservations(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 910018, 0, "platform")
	o := incomePaidOrder(t, s, 910018, "automatic-concurrent")
	const copies = 12
	results := make(chan Refund, copies)
	errs := make(chan error, copies)
	var wg sync.WaitGroup
	for range copies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.ReserveAPIRefund(t.Context(), o.MerchantID, o.OutTradeNo, "", 30)
			results <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal("unnumbered API refund was not reserved", e)
		}
	}
	var id int64
	for r := range results {
		if id == 0 {
			id = r.ID
		}
		if r.ID != id || r.RefundNo == "" {
			t.Fatalf("retries allocated different refunds: %d %+v", id, r)
		}
	}
	var count int
	if e := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM refunds WHERE order_id=$1", o.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate reservation", count, e)
	}
	b, e := s.Balance(t.Context(), o.MerchantID)
	if e != nil || b.Reserved != 30 {
		t.Fatal("duplicate reserved funds", b, e)
	}
}

func TestAutomaticAPIRefundWindowBoundaryWaitsForReservation(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 910019, 0, "platform")
	o := incomePaidOrder(t, s, 910019, "automatic-boundary")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	locked, release := make(chan struct{}), make(chan struct{})
	type result struct {
		r Refund
		e error
	}
	first, second := make(chan result, 1), make(chan result, 1)
	before := time.Unix(600000-1, 0)
	go func() {
		r, e := s.reserveAutomaticAPIRefund(ctx, o.MerchantID, o.OutTradeNo, 30, func() time.Time {
			close(locked)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return before
		})
		first <- result{r, e}
	}()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal("first reservation did not reach the locked clock")
	}
	go func() {
		r, e := s.reserveAutomaticAPIRefund(ctx, o.MerchantID, o.OutTradeNo, 30, func() time.Time { return before.Add(2 * time.Second) })
		second <- result{r, e}
	}()
	// The first identity has not been inserted yet. A copy must wait for that
	// reservation instead of selecting an empty identity from the next window.
	select {
	case got := <-second:
		close(release)
		t.Fatalf("copy bypassed the uncommitted reservation: %+v", got)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	a, b := <-first, <-second
	if a.e != nil || b.e != nil || a.r.ID != b.r.ID || a.r.RefundNo != b.r.RefundNo {
		t.Fatalf("boundary retry was not the original refund: %+v %+v", a, b)
	}
	var count int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM refunds WHERE order_id=$1", o.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("boundary retry reserved twice", count, e)
	}
	balance, e := s.Balance(ctx, o.MerchantID)
	if e != nil || balance.Reserved != 30 {
		t.Fatal("boundary retry funded twice", balance, e)
	}
}

func TestAutomaticAPIRefundRollbackFailureAndExplicitNumbers(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 910020, 0, "platform")
	o := incomePaidOrder(t, s, 910020, "automatic-recovery")
	now := time.Unix(600001, 0)
	canceled, cancel := context.WithCancel(t.Context())
	if _, e := s.reserveAutomaticAPIRefund(canceled, o.MerchantID, o.OutTradeNo, 30, func() time.Time { cancel(); return now }); !errors.Is(e, context.Canceled) {
		t.Fatal("canceled selection must roll back", e)
	}
	first, e := s.reserveAutomaticAPIRefund(t.Context(), o.MerchantID, o.OutTradeNo, 30, func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(t.Context(), first.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	retry, e := s.reserveAutomaticAPIRefund(t.Context(), o.MerchantID, o.OutTradeNo, 30, func() time.Time { return now })
	if e != nil || retry.ID == first.ID {
		t.Fatal("definitive failure must permit a new refund", retry, e)
	}
	other, e := s.reserveAutomaticAPIRefund(t.Context(), o.MerchantID, o.OutTradeNo, 20, func() time.Time { return now })
	if e != nil || other.ID == retry.ID {
		t.Fatal("different amount shared an identity", other, e)
	}
	explicit, e := s.ReserveAPIRefund(t.Context(), o.MerchantID, o.OutTradeNo, "explicit-request", 30)
	if e != nil || explicit.ID == retry.ID || explicit.RefundNo != "explicit-request" {
		t.Fatal("explicit refund number changed", explicit, e)
	}
	if _, e = s.ReserveRefund(t.Context(), o.MerchantID, o.OutTradeNo, "", 1); !errors.Is(e, ErrInvalid) {
		t.Fatal("internal refund unexpectedly accepted an absent number", e)
	}
}
