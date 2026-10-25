package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/db"
	"testing"
	"time"
)

func TestIncomeFundingAdoptionGrantRecomputedAfterOriginalPoolLockWait(t *testing.T) {
	s := incomeFundingFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := s.CreateMerchant(ctx, Merchant{ID: 6303, Name: "local pool lock", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	o, h := incomeLegacyHold(t, s, 6303, "first")
	_, other := incomeLegacyHold(t, s, 6303, "second")
	if e := db.Migrate(ctx, s.pool); e != nil {
		t.Fatal(e)
	}
	block, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(context.Background())
	var pid int32
	if e = block.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); e != nil {
		t.Fatal(e)
	}
	if _, e = block.Exec(ctx, "SELECT id FROM accounts WHERE owner_id=6303 AND kind='order_income_hold' FOR UPDATE"); e != nil {
		t.Fatal(e)
	}
	stale := errors.New("local stale pool snapshot")
	seen := make(chan int64, 1)
	guarded := WithFundsActionGuard(ctx, func(ctx context.Context, tx pgx.Tx, _, _ string) error {
		var n int64
		if e := tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=6303 AND kind='order_income_hold'").Scan(&n); e != nil {
			return e
		}
		seen <- n
		if n != 200 {
			return stale
		}
		return nil
	})
	done := make(chan error, 1)
	go func() {
		_, e := s.AdoptOrderIncomeHold(guarded, 6303, o.TradeNo, h.ID, "adopt-locked", IncomeHoldActor{ID: 1, Name: "local-super", Class: "super"})
		done <- e
	}()
	for {
		var n int
		if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid))", pid).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			break
		}
		select {
		case e = <-done:
			t.Fatal("adoption did not await original pool", e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if e = post(ctx, block, fmt.Sprintf("order-income:%d:unfreeze", other.ID), "real legacy release while waiting", 6303, "order_income_hold", 6303, "available", other.Amount); e != nil {
		t.Fatal(e)
	}
	if _, e = block.Exec(ctx, "UPDATE order_income_holds SET state='released',released_at=statement_timestamp() WHERE id=$1", other.ID); e != nil {
		t.Fatal(e)
	}
	other.State = "released"
	if e = incomeHoldAudit(ctx, block, other, "unfreeze", incomeTestActor); e != nil {
		t.Fatal(e)
	}
	if e = block.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	e = <-done
	actual := <-seen
	if actual != 100 || !errors.Is(e, stale) {
		t.Fatal("old pool authorization survived wait", actual, e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM order_income_hold_fundings WHERE hold_id=$1", h.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("denied adoption persisted", count, e)
	}
}
