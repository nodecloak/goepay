package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestMerchantPaymentReview82BlocksNewOrdersAndClaimsButPreservesOriginalOutcome(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 8282, Name: "review82", APIKey: "synthetic-review82-key"}); err != nil {
		t.Fatal(err)
	}
	var original int
	if err := s.pool.QueryRow(ctx, "SELECT pay FROM merchants WHERE id=8282").Scan(&original); err != nil || original != 1 {
		t.Fatal("old/new default admission must stay 1", original, err)
	}
	first, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 8282, OutTradeNo: "before-review82", Type: "wxpay", Amount: 125})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 8282, OutTradeNo: "original-attempt82", Type: "wxpay", Amount: 125})
	if err != nil {
		t.Fatal(err)
	}
	a, owner, err := s.BeginProviderAttempt(ctx, second.TradeNo, 7, "native")
	if err != nil || !owner {
		t.Fatal(a, owner, err)
	}
	for _, status := range []int{2, 0} {
		if _, err = s.pool.Exec(ctx, "UPDATE merchants SET pay=$1,profile=profile||'{\"pay\":\"1\"}'::jsonb WHERE id=8282", status); err != nil {
			t.Fatal(err)
		}
		_, err = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 8282, OutTradeNo: fmt.Sprint("blocked82-", status), Type: "wxpay", Amount: 125})
		if !errors.Is(err, ErrState) {
			t.Errorf("pay=%d accepted a new payment order: %v", status, err)
		}
		_, owner, err = s.BeginProviderAttempt(ctx, first.TradeNo, 7, "native")
		if !errors.Is(err, ErrState) || owner {
			t.Errorf("pay=%d accepted a first provider attempt: %v owner=%v", status, err, owner)
		}
		replayed, owned, err := s.BeginProviderAttempt(ctx, second.TradeNo, 7, "native")
		if err != nil || owned || replayed.ID != a.ID {
			t.Fatal("original attempt must remain recoverable without new ownership", err)
		}
	}
	if err = s.MarkPaid(ctx, second.TradeNo, "existing-outcome82"); err != nil {
		t.Fatal("an existing payment outcome must still be recorded", err)
	}
	balance, err := s.Balance(ctx, 8282)
	if err != nil || balance.Available != 125 {
		t.Fatal(balance, err)
	}
	if _, err = s.pool.Exec(ctx, "UPDATE merchants SET pay=1 WHERE id=8282"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 8282, OutTradeNo: "approved82", Type: "wxpay", Amount: 125}); err != nil {
		t.Fatal("approved merchant cannot receive", err)
	}
}
