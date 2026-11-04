package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestOrderNumber243TimeZoneAndSuffixBoundaries(t *testing.T) {
	// A UTC day boundary must use the same UTC+8 date even on a UTC server.
	at := time.Date(2026, 11, 3, 16, 2, 3, 0, time.UTC)
	for _, tc := range []struct {
		sequence int64
		want     string
	}{
		{1, "20261104000203" + "0000000000001"},
		{999999, "20261104000203" + "0000000999999"},
		{1000000, "20261104000203" + "0000001000000"},
		{maxOrderSequence243, "20261104000203" + "9999999999999"},
	} {
		got, err := orderTradeNumber243(at, tc.sequence)
		if err != nil || got != tc.want || len(got) != 27 || strings.Trim(got, "0123456789") != "" {
			t.Fatalf("sequence %d: got %q, want %q: %v", tc.sequence, got, tc.want, err)
		}
		if len(CombineSubTradeNo207(got, 50)) > 32 {
			t.Fatal("combined payment identifier exceeds 32 characters")
		}
	}
	for _, sequence := range []int64{-1, 0, maxOrderSequence243 + 1} {
		if _, err := orderTradeNumber243(at, sequence); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid sequence accepted", sequence, err)
		}
	}
	otherZone := at.In(time.FixedZone("UTC-7", -7*3600))
	if got, err := orderTradeNumber243(otherZone, 1); err != nil || got != "20261104000203"+"0000000000001" {
		t.Fatal("process/input timezone changed the system number", got, err)
	}
}

func TestOrderNumber243CombinedNamespaceDoesNotOverlap(t *testing.T) {
	at := time.Date(2026, 11, 4, 1, 2, 3, 0, time.UTC)
	first, err := orderTradeNumber243(at, 100000)
	if err != nil {
		t.Fatal(err)
	}
	second, err := orderTradeNumber243(at, 1000001)
	if err != nil {
		t.Fatal(err)
	}
	if CombineSubTradeNo207(first, 1) == second {
		t.Fatal("combined child overlaps another ordinary payment number")
	}
	if CombineSubTradeNo207(first, 11) == CombineSubTradeNo207(second, 1) {
		t.Fatal("children of different combined orders overlap")
	}
}

func TestOrderNumber243DatabaseIdentityCollisionsAndLimits(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	const mid = int64(24301)
	if err := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "Synthetic order number243", APIKey: "synthetic243"}); err != nil {
		t.Fatal(err)
	}
	var sharedBefore int64
	if err := s.pool.QueryRow(ctx, "SELECT last_value FROM trade_no_seq").Scan(&sharedBefore); err != nil {
		t.Fatal(err)
	}

	for _, collisions := range []int{2, 8} {
		t.Run(fmt.Sprintf("imported-collisions-%d", collisions), func(t *testing.T) {
			tx, err := s.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err = tx.Exec(ctx, "SELECT setval('order_trade_no_seq',1,false)"); err != nil {
				t.Fatal(err)
			}
			var at time.Time
			if err = tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&at); err != nil {
				t.Fatal(err)
			}
			prefix := at.In(time.FixedZone("UTC+8", 8*3600)).Format("20060102150405")
			for i := 1; i <= collisions; i++ {
				if _, err = tx.Exec(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount) VALUES($1,$2,$3,'Historical imported number','sandbox',100)`, fmt.Sprintf("%s%013d", prefix, i), mid, fmt.Sprintf("imported-%d", i)); err != nil {
					t.Fatal(err)
				}
			}
			in := CreateOrderInput{MerchantID: mid, OutTradeNo: "candidate243", Name: "Synthetic candidate", Type: "sandbox", Amount: 100, Version: "v1"}
			o, err := insertOrderRow243(ctx, tx, in, feeSnapshot{Policy: "none", Net: 100, Payer: "platform"}, false, nil)
			if collisions == 2 {
				if err != nil || o.TradeNo != prefix+"0000000000003" {
					t.Fatal("did not skip existing legacy numbers", o.TradeNo, err)
				}
			} else if !errors.Is(err, ErrConflict) {
				t.Fatal("collision retry was not bounded", err)
			}
			var count int
			if err = tx.QueryRow(ctx, "SELECT count(*) FROM orders WHERE out_trade_no LIKE 'imported-%'").Scan(&count); err != nil || count != collisions {
				t.Fatal("legacy rows changed or outer transaction was aborted", count, err)
			}
			var last int64
			if err = tx.QueryRow(ctx, "SELECT last_value FROM order_trade_no_seq").Scan(&last); err != nil || last != int64(min(collisions+1, 8)) {
				t.Fatal("wrong retry sequence consumption", last, err)
			}
		})
	}

	t.Run("unrelated-constraint-is-not-retried", func(t *testing.T) {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(ctx, "SELECT setval('order_trade_no_seq',100,false)"); err != nil {
			t.Fatal(err)
		}
		_, err = insertOrderRow243(ctx, tx, CreateOrderInput{MerchantID: 24399, OutTradeNo: "missing-merchant", Name: "Synthetic", Type: "sandbox", Amount: 100, Version: "v1"}, feeSnapshot{Policy: "none", Net: 100, Payer: "platform"}, false, nil)
		var constraint *pgconn.PgError
		if !errors.As(err, &constraint) || constraint.Code != "23503" {
			t.Fatal("foreign key error was hidden or changed", err)
		}
		var last int64
		if err = tx.QueryRow(ctx, "SELECT last_value FROM order_trade_no_seq").Scan(&last); err != nil || last != 100 {
			t.Fatal("unrelated constraint retried or aborted outer transaction", last, err)
		}
	})

	t.Run("legacy-order-replay-keeps-original-number", func(t *testing.T) {
		const legacy = "1000000000000000243"
		if _, err := s.pool.Exec(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,fee_policy) VALUES($1,$2,'legacy243','Historical goods','sandbox',100,100,'none')`, legacy, mid); err != nil {
			t.Fatal(err)
		}
		var before string
		if err := s.pool.QueryRow(ctx, "SELECT to_jsonb(orders)::text FROM orders WHERE trade_no=$1", legacy).Scan(&before); err != nil {
			t.Fatal(err)
		}
		o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "legacy243", Name: "Historical goods", Type: "sandbox", Amount: 100})
		if err != nil || o.TradeNo != legacy {
			t.Fatal("legacy replay was renumbered", o.TradeNo, err)
		}
		var after string
		if err := s.pool.QueryRow(ctx, "SELECT to_jsonb(orders)::text FROM orders WHERE trade_no=$1", legacy).Scan(&after); err != nil || before != after {
			t.Fatal("legacy order facts changed", err)
		}
	})

	t.Run("other-unique-constraint-is-not-retried", func(t *testing.T) {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(ctx, `CREATE UNIQUE INDEX order_number243_other_unique ON orders(name) WHERE name='Unique item243'`); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount) VALUES('unrelated-unique243',$1,'other-unique-first','Unique item243','sandbox',100)`, mid); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "SELECT setval('order_trade_no_seq',200,false)"); err != nil {
			t.Fatal(err)
		}
		_, err = insertOrderRow243(ctx, tx, CreateOrderInput{MerchantID: mid, OutTradeNo: "other-unique-second", Name: "Unique item243", Type: "sandbox", Amount: 100, Version: "v1"}, feeSnapshot{Policy: "none", Net: 100, Payer: "platform"}, false, nil)
		var constraint *pgconn.PgError
		if !errors.As(err, &constraint) || constraint.Code != "23505" || constraint.ConstraintName != "order_number243_other_unique" {
			t.Fatal("another unique constraint was hidden or changed", err)
		}
		var last int64
		if err = tx.QueryRow(ctx, "SELECT last_value FROM order_trade_no_seq").Scan(&last); err != nil || last != 200 {
			t.Fatal("another unique constraint was retried or aborted outer transaction", last, err)
		}
	})

	t.Run("concurrent-independent-stores", func(t *testing.T) {
		other, err := NewWithKey(s.pool, make([]byte, 32))
		if err != nil {
			t.Fatal(err)
		}
		type result struct {
			order Order
			err   error
		}
		results := make(chan result, 24)
		var wg sync.WaitGroup
		for i := 0; i < 24; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				store := s
				if i%2 != 0 {
					store = other
				}
				o, err := store.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("parallel243-%d", i), Name: "Synthetic", Type: "sandbox", Amount: 100})
				results <- result{o, err}
			}(i)
		}
		wg.Wait()
		close(results)
		seen := map[string]bool{}
		for r := range results {
			want := r.order.CreatedAt.In(time.FixedZone("UTC+8", 8*3600)).Format("20060102150405")
			if r.err != nil || !strings.HasPrefix(r.order.TradeNo, want) || seen[r.order.TradeNo] {
				t.Fatal("new identifiers were not timestamped and unique", r.order.TradeNo, r.err)
			}
			seen[r.order.TradeNo] = true
		}
	})

	t.Run("maximum-parent-combined-children-and-exhaustion", func(t *testing.T) {
		if _, err := s.pool.Exec(ctx, "SELECT setval('order_trade_no_seq',$1,false)", maxOrderSequence243); err != nil {
			t.Fatal(err)
		}
		o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "maximum243", Name: "Synthetic combined", Type: "wxpay", Amount: 100})
		if err != nil || len(o.TradeNo) != 27 {
			t.Fatal("maximum sequence lost digits", o.TradeNo, err)
		}
		amounts := make([]int64, 50)
		for i := range amounts {
			amounts[i] = 2
		}
		subs, err := s.CreateCombineSubOrders(ctx, o.ID, o.TradeNo, amounts)
		if err != nil || len(subs) != 50 || len(subs[49].SubTradeNo) != 29 {
			t.Fatal("maximum order number did not support combined children", err)
		}
		if _, err = s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "exhausted243", Name: "Synthetic", Type: "sandbox", Amount: 100}); err == nil {
			t.Fatal("exhausted sequence cycled or fell back")
		}
		var count int
		if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM orders WHERE out_trade_no='exhausted243'").Scan(&count); err != nil || count != 0 {
			t.Fatal("exhausted sequence left an order", count, err)
		}
	})
	var sharedAfter int64
	if err := s.pool.QueryRow(ctx, "SELECT last_value FROM trade_no_seq").Scan(&sharedAfter); err != nil || sharedAfter != sharedBefore {
		t.Fatal("new payment orders consumed the old transfer/settlement sequence", sharedAfter, err)
	}
}
