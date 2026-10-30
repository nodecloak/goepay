package payment

import (
	"context"
	"errors"
	"testing"
)

func TestDepositMinimumGateRefusesPaymentOrdersBelowMinimum(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 92, Name: "deposit minimum", APIKey: "test-key-long"}); e != nil {
		t.Fatal(e)
	}
	// No 保证金 setting: payment orders are accepted, as today.
	fund, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 92, OutTradeNo: "fund92", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, fund.TradeNo, "fund92"); e != nil {
		t.Fatal(e)
	}
	setting := func(key, value string) {
		t.Helper()
		tag, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_build_object('key',$1::text,'value',$2::text) WHERE resource='settings' AND body->>'key'=$1`, key, value)
		if e != nil {
			t.Fatal(e)
		}
		if tag.RowsAffected() == 0 {
			if _, e = s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, key, value); e != nil {
				t.Fatal(e)
			}
		}
	}
	// A 10.00 yuan minimum with nothing held refuses the next payment order.
	setting("user_deposit_min", "10.00")
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 92, OutTradeNo: "blocked92", Amount: 100}); !errors.Is(e, ErrDepositBelowMinimum) || !errors.Is(e, ErrState) {
		t.Fatalf("payment below the deposit minimum: %v", e)
	}
	// Holding exactly the minimum lets payments through again.
	if _, e = s.Deposit(ctx, 92, "hold", "hold92", 1000, "merchant:92"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 92, OutTradeNo: "after-hold92", Amount: 100}); e != nil {
		t.Fatalf("payment at the deposit minimum: %v", e)
	}
	// Raising the minimum above the held deposit refuses again; switching the
	// deposit feature off lifts the requirement.
	setting("user_deposit_min", "20.00")
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 92, OutTradeNo: "raised92", Amount: 100}); !errors.Is(e, ErrDepositBelowMinimum) {
		t.Fatalf("payment below a raised minimum: %v", e)
	}
	setting("user_deposit", "0")
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 92, OutTradeNo: "feature-off92", Amount: 100}); e != nil {
		t.Fatalf("payment with the deposit feature off: %v", e)
	}
	// A malformed minimum is read as no requirement, never as a refusal.
	setting("user_deposit", "1")
	setting("user_deposit_min", "abc")
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 92, OutTradeNo: "malformed92", Amount: 100}); e != nil {
		t.Fatalf("payment with a malformed minimum: %v", e)
	}
}
