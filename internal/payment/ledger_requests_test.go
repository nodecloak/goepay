package payment

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestInternalTransfersAndSettlementReservedApproval(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, id := range []int64{80, 81} {
		if e := s.CreateMerchant(ctx, Merchant{ID: id, Name: "ledger requests", APIKey: "test-secret"}); e != nil {
			t.Fatal(e)
		}
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 80, OutTradeNo: "fund", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "fund-request"); e != nil {
		t.Fatal(e)
	}
	request, e := s.RequestFunds(ctx, "internal-transfers", 80, 81, "r1", 300, "merchant:80")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := s.Balance(ctx, 80)
	if b.Available != 700 || b.Reserved != 300 {
		t.Fatalf("reserve %+v", b)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.ReviewFundsRequest(ctx, request.ID, true, "admin:1"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	b, _ = s.Balance(ctx, 80)
	recipient, _ := s.Balance(ctx, 81)
	if b.Available != 700 || b.Reserved != 0 || recipient.Available != 300 {
		t.Fatalf("transfer source%+v recipient%+v", b, recipient)
	}
	if _, e = s.ReviewFundsRequest(ctx, request.ID, false, "admin:1"); !errors.Is(e, ErrState) {
		t.Fatalf("approved reversed %v", e)
	}
	same, e := s.RequestFunds(ctx, "internal-transfers", 80, 81, "r1", 300, "merchant:80")
	if e != nil || same.ID != request.ID {
		t.Fatalf("retry %+v %v", same, e)
	}
	settlement, e := s.RequestFunds(ctx, "settlements", 80, 0, "settle1", 200, "merchant:80")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReviewFundsRequest(ctx, settlement.ID, true, "admin:1"); e != nil {
		t.Fatal(e)
	}
	b, _ = s.Balance(ctx, 80)
	if b.Available != 500 || b.Reserved != 200 {
		t.Fatalf("settlement approval must retain reservation %+v", b)
	}
	reject, e := s.RequestFunds(ctx, "settlements", 80, 0, "settle2", 100, "merchant:80")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReviewFundsRequest(ctx, reject.ID, false, "admin:1"); e != nil {
		t.Fatal(e)
	}
	b, _ = s.Balance(ctx, 80)
	if b.Available != 500 || b.Reserved != 200 {
		t.Fatalf("reject %+v", b)
	}
	if _, e = s.pool.Exec(ctx, "DELETE FROM ledger_request_events"); e == nil {
		t.Fatal("review audit mutable")
	}
}
