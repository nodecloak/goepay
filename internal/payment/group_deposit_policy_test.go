package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func depositPolicyGroup(t *testing.T, s *Store, body string) int64 {
	t.Helper()
	var id int64
	if e := s.pool.QueryRow(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('groups',$1::jsonb) RETURNING id`, body).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}
func depositPolicyFund(t *testing.T, s *Store, mid int64) {
	t.Helper()
	if e := s.CreateMerchant(t.Context(), Merchant{ID: mid, Name: "synthetic group deposit", APIKey: "synthetic-deposit-policy-key"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprint("deposit-fund-", mid), Amount: 10000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(t.Context(), o.TradeNo, fmt.Sprint("deposit-fund-paid-", mid)); e != nil {
		t.Fatal(e)
	}
}
func TestGroupDepositPolicyAppliesMinimumReleaseWindowSwitchAndOriginalReplay(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	const mid int64 = 123001
	depositPolicyFund(t, s, mid)
	gid := depositPolicyGroup(t, s, `{"name":"deposit base","enabled":true,"user_deposit":"1","user_deposit_min":"10.00","user_deposit_day":"7"}`)
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$2 WHERE id=$1`, mid, gid); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "group-deposit-below", Amount: 100}); !errors.Is(e, ErrDepositBelowMinimum) {
		t.Errorf("group minimum did not gate new order: %v", e)
	}
	held, e := s.Deposit(ctx, mid, "hold", "group-deposit-hold", 1000, "merchant:test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "group-deposit-at-minimum", Amount: 100}); e != nil {
		t.Fatal(e)
	}
	released, e := s.Deposit(ctx, mid, "release", "group-deposit-frozen", 100, "merchant:test")
	if e == nil {
		t.Error("group freeze days allowed release with a recent order")
		if _, e = s.ReviewDeposit(ctx, released.ID, false, "admin:test"); e != nil {
			t.Fatal(e)
		}
	}
	member := depositPolicyGroup(t, s, `{"name":"deposit member","enabled":true,"user_deposit":"1","user_deposit_min":"0","user_deposit_day":"0"}`)
	if _, e = s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '1 hour')`, mid, member); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"user_deposit","value":"0"}'),('settings','{"key":"user_deposit_min","value":"99.00"}'),('settings','{"key":"user_deposit_day","value":"30"}')`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "group-deposit-member-zero", Amount: 100}); e != nil {
		t.Errorf("explicit zero minimum did not override global: %v", e)
	}
	released, e = s.Deposit(ctx, mid, "release", "group-deposit-member-release", 100, "merchant:test")
	if e != nil {
		t.Errorf("explicit zero days did not override global: %v", e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"user_deposit":"0"}'::jsonb WHERE resource='groups' AND id=$1`, member); e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"hold", "release"} {
		if _, e = s.Deposit(ctx, mid, action, "group-deposit-off-"+action, 100, "merchant:test"); e == nil {
			t.Errorf("closed effective group allowed new %s", action)
		}
	}
	replay, e := s.Deposit(ctx, mid, "hold", "group-deposit-hold", 1000, "merchant:test")
	if e != nil || replay.ID != held.ID {
		t.Errorf("feature closure blocked original hold replay: %v", e)
	}
	if released.ID > 0 {
		if _, e = s.ReviewDeposit(ctx, released.ID, true, "admin:test"); e != nil {
			t.Errorf("group closure blocked administrator original review: %v", e)
		}
	}
	if _, e = s.pool.Exec(ctx, `UPDATE memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE merchant_id=$1`, mid); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "group-deposit-expired-base", Amount: 100}); !errors.Is(e, ErrDepositBelowMinimum) {
		t.Errorf("expired membership did not restore base minimum: %v", e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprint("deposit-fund-", mid), Amount: 10000}); e != nil {
		t.Errorf("raised current minimum blocked an existing order replay: %v", e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"user_deposit_min":true}'::jsonb WHERE resource='groups' AND id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "group-deposit-bad-new-order", Amount: 100}); e == nil {
		t.Error("malformed group deposit policy allowed new order")
	}
	if _, e = s.Deposit(ctx, mid, "hold", "group-deposit-bad-new-hold", 100, "merchant:test"); e == nil {
		t.Error("malformed group deposit policy allowed new hold")
	}
	if replay, e = s.Deposit(ctx, mid, "hold", "group-deposit-hold", 1000, "merchant:test"); e != nil || replay.ID != held.ID {
		t.Errorf("malformed current policy blocked original hold replay: %v", e)
	}
}

func TestGroupDepositPolicyRechecksMembershipAfterFundsGuardAndLedgerWait(t *testing.T) {
	s := testStore(t)
	for i, stage := range []string{"funds-guard", "ledger-post"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			mid := int64(123010 + i)
			depositPolicyFund(t, s, mid)
			base := depositPolicyGroup(t, s, fmt.Sprintf(`{"name":"deposit closed base %d","enabled":true,"user_deposit":"0"}`, mid))
			member := depositPolicyGroup(t, s, fmt.Sprintf(`{"name":"deposit expiring member %d","enabled":true,"user_deposit":"1"}`, mid))
			if _, e := s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$2 WHERE id=$1`, mid, base); e != nil {
				t.Fatal(e)
			}
			var deadline time.Time
			if e := s.pool.QueryRow(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '3 seconds') RETURNING expires_at`, mid, member).Scan(&deadline); e != nil {
				t.Fatal(e)
			}
			var journals int
			if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM journals`).Scan(&journals); e != nil {
				t.Fatal(e)
			}
			wait := func() {
				t.Helper()
				if _, e := s.pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM ($1::timestamptz-clock_timestamp()))::double precision)+0.05)`, deadline); e != nil {
					t.Fatal(e)
				}
			}
			var e error
			if stage == "funds-guard" {
				called := false
				guarded := WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
					called = true
					_, e := tx.Exec(c, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM ($1::timestamptz-clock_timestamp()))::double precision)+0.05)`, deadline)
					return e
				})
				_, e = s.Deposit(guarded, mid, "hold", "expire-after-guard", 100, "merchant:test")
				if !called {
					t.Fatal("guard wait was not exercised")
				}
			} else {
				blocker, err := s.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer blocker.Rollback(ctx)
				if _, err = blocker.Exec(ctx, `SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' FOR UPDATE`, mid); err != nil {
					t.Fatal(err)
				}
				reached := make(chan struct{})
				result := make(chan error, 1)
				guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { close(reached); return nil })
				go func() {
					_, err := s.Deposit(guarded, mid, "hold", "expire-after-post", 100, "merchant:test")
					result <- err
				}()
				select {
				case <-reached:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				wait()
				if err = blocker.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case e = <-result:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if e == nil {
				t.Error("expired membership still committed a new deposit intent")
			}
			var actions, after int
			if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM deposit_actions WHERE merchant_id=$1),(SELECT count(*) FROM journals)`, mid).Scan(&actions, &after); err != nil {
				t.Fatal(err)
			}
			balance, err := s.Balance(ctx, mid)
			if err != nil || actions != 0 || after != journals || balance.Available != 10000 {
				t.Errorf("expired intent retained local ledger changes: actions=%d journals=%d/%d balance=%+v err=%v", actions, after, journals, balance, err)
			}
		})
	}
}
