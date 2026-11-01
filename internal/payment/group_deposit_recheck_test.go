package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func depositPolicyWaitForLock(t *testing.T, ctx context.Context, s *Store, name string) {
	t.Helper()
	for {
		var waiting bool
		if e := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND wait_event_type='Lock')`, name).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestGroupDepositPolicyRejectsFirstGlobalInsertAfterGuardAndPost(t *testing.T) {
	s := testStore(t)
	for index, tc := range []struct{ name, action, key, value string }{{"guard-disabled", "hold", "user_deposit", "0"}, {"guard-presence", "hold", "user_deposit_day", "0"}, {"post-days", "release", "user_deposit_day", "7"}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			mid := int64(123020 + index)
			if _, e := s.pool.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key' IN ('user_deposit','user_deposit_min','user_deposit_day')`); e != nil {
				t.Fatal(e)
			}
			depositPolicyFund(t, s, mid)
			if tc.action == "release" {
				if _, e := s.Deposit(ctx, mid, "hold", "first-global-seed", 1000, "merchant:test"); e != nil {
					t.Fatal(e)
				}
			}
			held, pending, e := s.DepositBalance(ctx, mid)
			if e != nil {
				t.Fatal(e)
			}
			var actions, journals int
			if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM deposit_actions WHERE merchant_id=$1),(SELECT count(*) FROM journals)`, mid).Scan(&actions, &journals); e != nil {
				t.Fatal(e)
			}
			insert := func() error {
				_, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, tc.key, tc.value)
				return e
			}
			if tc.name != "post-days" {
				guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return insert() })
				_, e = s.Deposit(guarded, mid, tc.action, "first-global-"+tc.name, 100, "merchant:test")
			} else {
				blocker, err := s.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer blocker.Rollback(ctx)
				if _, err = blocker.Exec(ctx, `SELECT id FROM accounts WHERE owner_id=$1 AND kind='deposit' FOR UPDATE`, mid); err != nil {
					t.Fatal(err)
				}
				name := fmt.Sprint("group-deposit-post-", mid)
				reached := make(chan struct{})
				result := make(chan error, 1)
				guarded := WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
					if _, err := tx.Exec(c, `SELECT set_config('application_name',$1,true)`, name); err != nil {
						return err
					}
					close(reached)
					return nil
				})
				go func() {
					_, err := s.Deposit(guarded, mid, tc.action, "first-global-post", 100, "merchant:test")
					result <- err
				}()
				select {
				case <-reached:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				depositPolicyWaitForLock(t, ctx, s, name)
				if err = insert(); err != nil {
					t.Fatal(err)
				}
				if err = blocker.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case e = <-result:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if !errors.Is(e, ErrConflict) {
				t.Errorf("new global deposit policy inserted after snapshot was not rejected as stale intent: %v", e)
			}
			var afterActions, afterJournals int
			if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM deposit_actions WHERE merchant_id=$1),(SELECT count(*) FROM journals)`, mid).Scan(&afterActions, &afterJournals); err != nil {
				t.Fatal(err)
			}
			afterHeld, afterPending, err := s.DepositBalance(ctx, mid)
			if err != nil || afterActions != actions || afterJournals != journals || afterHeld != held || afterPending != pending {
				t.Errorf("late global change retained ledger effect: actions=%d/%d journals=%d/%d held=%d/%d pending=%d/%d err=%v", afterActions, actions, afterJournals, journals, afterHeld, held, afterPending, pending, err)
			}
		})
	}
}

func TestGroupDepositPolicyRejectsFirstGlobalMinimumBeforeOrderCommit(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const mid int64 = 123040
	if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "synthetic order global recheck", APIKey: "synthetic-global-recheck"}); e != nil {
		t.Fatal(e)
	}
	const name = "group-deposit-order-123040"
	ddl := `CREATE FUNCTION deposit_order_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM set_config('application_name','group-deposit-order-123040',true); PERFORM pg_advisory_xact_lock(123040::bigint); RETURN NEW; END $$; CREATE TRIGGER deposit_order_barrier BEFORE INSERT ON orders FOR EACH ROW WHEN (NEW.out_trade_no='first-global-minimum-order') EXECUTE FUNCTION deposit_order_barrier()`
	if _, e := s.pool.Exec(ctx, ddl); e != nil {
		t.Fatal(e)
	}
	blocker, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(ctx)
	if _, e = blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(123040::bigint)`); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() {
		_, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "first-global-minimum-order", Amount: 100})
		result <- e
	}()
	depositPolicyWaitForLock(t, ctx, s, name)
	if _, e = s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"user_deposit_min","value":"99.00"}')`); e != nil {
		t.Fatal(e)
	}
	if e = blocker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-result:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if !errors.Is(e, ErrConflict) {
		t.Errorf("new order was not rejected as stale policy after its captured global minimum changed: %v", e)
	}
	if _, e = s.Order(ctx, mid, "first-global-minimum-order"); !errors.Is(e, ErrNotFound) {
		t.Errorf("stale-policy order survived rollback: %v", e)
	}
}
