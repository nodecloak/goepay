package payment

import (
	"context"
	"errors"
	"math"
	"strconv"
	"sync"
	"testing"
)

func feeMerchant(t *testing.T, s *Store, merchantID int64, rate int64, payer string) {
	t.Helper()
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: merchantID, Name: "fees", APIKey: "test-only-fee-key"}); e != nil {
		t.Fatal(e)
	}
	var groupID int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups',jsonb_build_object('name','fee-group','enabled',true,'rate_bps',$1::bigint::text)) RETURNING id`, rate).Scan(&groupID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_build_object('group_id',$2::bigint::text) WHERE id=$1`, merchantID, groupID); e != nil {
		t.Fatal(e)
	}
	for _, pair := range [][2]string{{"payment_fee_enabled", "true"}, {"refund_fee_payer", payer}} {
		if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text)) ON CONFLICT DO NOTHING`, pair[0], pair[1]); e != nil {
			t.Fatal(e)
		}
	}
}
func TestFixedPointFeeRetainedRoundingAndOverflow(t *testing.T) {
	for _, tc := range []struct{ gross, rate, net, fee int64 }{{100, 1000, 90, 10}, {1, 5000, 1, 0}, {101, 1000, 91, 10}, {math.MaxInt64, 0, math.MaxInt64, 0}, {math.MaxInt64, 10000, 0, math.MaxInt64}} {
		net, fee, e := calculateFee(tc.gross, tc.rate)
		if e != nil || net != tc.net || fee != tc.fee {
			t.Fatalf("fee %+v -> %d,%d,%v", tc, net, fee, e)
		}
	}
	if _, _, e := calculateFee(100, -1); !errors.Is(e, ErrInvalid) {
		t.Fatalf("invalidrate %v", e)
	}
}
func TestOrderFeeSnapshotAndConcurrentNetCredit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 301, 1000, "platform")
	in := CreateOrderInput{MerchantID: 301, OutTradeNo: "fee", Amount: 100, Name: "item"}
	o, e := s.CreateOrder(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if o.MerchantAmount != 90 || o.PlatformFee != 10 || o.FeeRateBPS != 1000 {
		t.Fatalf("snapshot %+v", o)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{rate_bps}','"5000"') WHERE resource='groups'`); e != nil {
		t.Fatal(e)
	}
	same, e := s.CreateOrder(ctx, in)
	if e != nil || same.TradeNo != o.TradeNo || same.MerchantAmount != 90 {
		t.Fatalf("recomputed idempotent fee %+v %v", same, e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.MarkPaid(ctx, o.TradeNo, "fee-paid"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	b, _ := s.Balance(ctx, 301)
	var fee int64
	if e = s.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees'").Scan(&fee); e != nil || fee != 10 || b.Available != 90 {
		t.Fatalf("net%+v fee%d %v", b, fee, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE orders SET merchant_amount=amount,platform_fee=0 WHERE id=$1", o.ID); e == nil {
		t.Fatal("fee snapshot mutable")
	}
}
func TestRefundFeeAllocationHandlesFailedReservationHoles(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 302, 1000, "platform")
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 302, OutTradeNo: "fee-refund", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "fee-refund-paid"); e != nil {
		t.Fatal(e)
	}
	a, e := s.ReserveRefund(ctx, 302, o.OutTradeNo, "a", 90)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.ReserveRefund(ctx, 302, o.OutTradeNo, "b", 10)
	if e != nil {
		t.Fatal(e)
	}
	if a.MerchantAmount != 90 || a.FeeAmount != 0 || b.MerchantAmount != 0 || b.FeeAmount != 10 {
		t.Fatalf("alloc a%+v b%+v", a, b)
	}
	if e = s.ResolveRefund(ctx, b.ID, "unknown"); e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, a.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	c, e := s.ReserveRefund(ctx, 302, o.OutTradeNo, "c", 90)
	if e != nil || c.MerchantAmount != 90 || c.FeeAmount != 0 {
		t.Fatalf("failure hole %+v %v", c, e)
	}
	if _, e = s.ReserveRefund(ctx, 302, o.OutTradeNo, "over", 1); !errors.Is(e, ErrInvalid) {
		t.Fatalf("gross overrefund %v", e)
	}
	if e = s.ResolveRefund(ctx, b.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, c.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	balance, _ := s.Balance(ctx, 302)
	var fees, held int64
	e = s.pool.QueryRow(ctx, "SELECT COALESCE(sum(balance)FILTER(WHERE kind='fees'),0),COALESCE(sum(balance)FILTER(WHERE kind='fee_reserved'),0) FROM accounts WHERE owner_id=0").Scan(&fees, &held)
	if e != nil || balance.Available != 0 || balance.Reserved != 0 || fees != 0 || held != 0 {
		t.Fatalf("refund net%+v fee%d reserved%d %v", balance, fees, held, e)
	}
}
func TestMerchantPaysRetainedFeeOnRefund(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 303, 1000, "merchant")
	var first Order
	for _, n := range []string{"first", "fund"} {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 303, OutTradeNo: n, Amount: 100})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.MarkPaid(ctx, o.TradeNo, n); e != nil {
			t.Fatal(e)
		}
		if n == "first" {
			first = o
		}
	}
	r, e := s.ReserveRefund(ctx, 303, first.OutTradeNo, "full", 100)
	if e != nil || r.MerchantAmount != 100 || r.FeeAmount != 0 {
		t.Fatalf("merchant payer %+v %v", r, e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	b, _ := s.Balance(ctx, 303)
	var fees int64
	s.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees'").Scan(&fees)
	if b.Available != 80 || fees != 20 {
		t.Fatalf("retained fee net%+v fees%d", b, fees)
	}
}

func TestConcurrentPartialRefundsCapMerchantAndPlatformFee(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 304, 1000, "platform")
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 304, OutTradeNo: "race", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "race-fee"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r, e := s.ReserveRefund(ctx, 304, o.OutTradeNo, strconv.Itoa(n), 5)
			if e != nil {
				t.Error(e)
				return
			}
			if e = s.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	var gross, merchant, fee int64
	e = s.pool.QueryRow(ctx, "SELECT sum(amount),sum(merchant_amount),sum(fee_amount) FROM refunds WHERE order_id=$1 AND status='succeeded'", o.ID).Scan(&gross, &merchant, &fee)
	if e != nil || gross != 100 || merchant != 90 || fee != 10 {
		t.Fatalf("concurrent gross%d merchant%d fee%d %v", gross, merchant, fee, e)
	}
	b, _ := s.Balance(ctx, 304)
	if b.Available != 0 || b.Reserved != 0 {
		t.Fatalf("refund final %+v", b)
	}
}
