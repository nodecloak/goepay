package payment

import (
	"context"
	"errors"
	"testing"
)

func combine207Order(t *testing.T, s *Store, amount int64) Order {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 920207, Name: "combine207", APIKey: "test-only"}); err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 920207, OutTradeNo: "combine207", Name: "item", Type: "wxpay", Amount: amount})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestCombineSubOrders207RecordIdempotentBalancedAndImmutable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	o := combine207Order(t, s, 10000)
	subs, err := s.CreateCombineSubOrders(ctx, o.ID, o.TradeNo, []int64{3334, 3333, 3333})
	if err != nil || len(subs) != 3 || subs[0].SubTradeNo != o.TradeNo+"1" || subs[2].SubTradeNo != o.TradeNo+"3" || subs[0].Status != "pending" {
		t.Fatal("sub-orders not recorded with original numbering", subs, err)
	}
	if again, err := s.CreateCombineSubOrders(ctx, o.ID, o.TradeNo, []int64{3334, 3333, 3333}); err != nil || len(again) != 3 {
		t.Fatal("identical split was not idempotent", err)
	}
	if _, err := s.CreateCombineSubOrders(ctx, o.ID, o.TradeNo, []int64{5000, 2500, 2500}); !errors.Is(err, ErrConflict) {
		t.Fatal("a different split was accepted", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE combine_suborders207 SET amount=amount+1 WHERE order_id=$1 AND position=1`, o.ID); err == nil {
		t.Fatal("sub-order amount was mutable")
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM combine_suborders207 WHERE order_id=$1`, o.ID); err == nil {
		t.Fatal("sub-orders could be deleted")
	}
}

func TestCombineSubOrders207MustSumToOrderAmount(t *testing.T) {
	s := testStore(t)
	o := combine207Order(t, s, 10000)
	if _, err := s.CreateCombineSubOrders(context.Background(), o.ID, o.TradeNo, []int64{3333, 3333, 3333}); err == nil {
		t.Fatal("unbalanced split was committed")
	}
	if subs, _ := s.CombineSubOrders(context.Background(), o.ID); len(subs) != 0 {
		t.Fatal("rejected split left rows", subs)
	}
}

func TestCombineSubOrders207PaidRequiresEveryShareExactlyOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	o := combine207Order(t, s, 10000)
	if _, err := s.CreateCombineSubOrders(ctx, o.ID, o.TradeNo, []int64{3334, 3333, 3333}); err != nil {
		t.Fatal(err)
	}
	mark := func(paid []CombinePaid207) error {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err = s.MarkCombineSubOrdersPaidTx(ctx, tx, o.ID, paid); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	full := []CombinePaid207{{o.TradeNo + "1", "WX1", 3334}, {o.TradeNo + "2", "WX2", 3333}, {o.TradeNo + "3", "WX3", 3333}}
	if err := mark(full[:2]); err == nil {
		t.Fatal("partial combined payment marked sub-orders paid")
	}
	wrong := append([]CombinePaid207(nil), full...)
	wrong[1].Amount = 3332
	if err := mark(wrong); !errors.Is(err, ErrConflict) {
		t.Fatal("wrong sub-order amount accepted", err)
	}
	if err := mark(full); err != nil {
		t.Fatal(err)
	}
	if err := mark(full); err != nil {
		t.Fatal("repeat of the same payment was not idempotent", err)
	}
	other := append([]CombinePaid207(nil), full...)
	other[0].TransactionID = "WX9"
	if err := mark(other); !errors.Is(err, ErrConflict) {
		t.Fatal("different transaction id accepted for a paid sub-order", err)
	}
	subs, _ := s.CombineSubOrders(ctx, o.ID)
	for _, sub := range subs {
		if sub.Status != "paid" || sub.TransactionID == "" || sub.PaidAt == nil {
			t.Fatal("sub-order not paid", sub)
		}
	}
}
