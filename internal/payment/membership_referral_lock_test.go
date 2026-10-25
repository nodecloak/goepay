package payment

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func membership72WaitClock(t *testing.T, s *Store, at time.Time) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		var reached bool
		if e := s.pool.QueryRow(context.Background(), "SELECT pg_catalog.clock_timestamp()>=$1", at).Scan(&reached); e != nil {
			t.Fatal(e)
		}
		if reached {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("local DB clock did not advance")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func membership72WaitAccount(t *testing.T, s *Store) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if e := s.pool.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT id FROM accounts WHERE id=ANY%')").Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("purchase did not wait for original wallet lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func membership72ExpiringGroup(t *testing.T, s *Store, buyer int64) time.Time {
	t.Helper()
	ctx := context.Background()
	group := membership72Group(t, s, 10)
	rate := int64(5000)
	if _, e := s.SetMembershipReferralPolicy(ctx, "group", group, 0, MembershipReferralPolicyInput{RateBPS: &rate}, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	var expiry time.Time
	if e := s.pool.QueryRow(ctx, "INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES($1,$2,pg_catalog.clock_timestamp()+interval '2 seconds') RETURNING expires_at", buyer, group).Scan(&expiry); e != nil {
		t.Fatal(e)
	}
	return expiry
}

func TestMembershipReferral72AccountWaitRevalidatesOriginalMembershipExpiry(t *testing.T) {
	s, _, buyer, group := membership72RewardFixture(t)
	ctx := context.Background()
	expiry := membership72ExpiringGroup(t, s, buyer)
	locker, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer locker.Rollback(ctx)
	var account int64
	if e = locker.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0 FOR UPDATE", buyer).Scan(&account); e != nil {
		t.Fatal(e)
	}
	var grants atomic.Int32
	guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { grants.Add(1); return nil })
	done := make(chan error, 1)
	go func() {
		_, e := s.PurchaseMembership(guarded, buyer, group, "member72-expired-while-account-wait", "merchant:7211")
		done <- e
	}()
	membership72WaitAccount(t, s)
	membership72WaitClock(t, s, expiry)
	if e = locker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-done; !errors.Is(e, ErrConflict) {
		t.Fatal("stale pre-lock group policy priced membership", e)
	}
	var purchases, accounts int64
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM membership_purchases),(SELECT count(*) FROM accounts WHERE kind='membership_income')").Scan(&purchases, &accounts); e != nil || purchases != 0 || accounts != 0 || grants.Load() != 0 {
		t.Fatal("stale group partially purchased or consumed authorization", purchases, accounts, grants.Load(), e)
	}
}

func TestMembershipReferral72FundsGuardWaitRevalidatesExpiryBeforePurchase(t *testing.T) {
	s, _, buyer, group := membership72RewardFixture(t)
	ctx := context.Background()
	expiry := membership72ExpiringGroup(t, s, buyer)
	var calls int
	guarded := WithFundsActionGuard(ctx, func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		calls++
		if kind != "membership" || !strings.Contains(key, "member72-expired-at-grant") {
			return ErrState
		}
		membership72WaitClock(t, s, expiry)
		return nil
	})
	if _, e := s.PurchaseMembership(guarded, buyer, group, "member72-expired-at-grant", "merchant:7211"); !errors.Is(e, ErrConflict) {
		t.Fatal("expired group accepted after financial guard wait", e)
	}
	if calls != 1 {
		t.Fatal("original purchase authorization was not checked", calls)
	}
	b, e := s.Balance(ctx, buyer)
	if e != nil || b.Available != 1000 {
		t.Fatal("expired quote partially charged", b, e)
	}
}

func TestMembershipReferral72InitialExpiryUsesLockAfterDatabaseClock(t *testing.T) {
	s, _, buyer, group := membership72RewardFixture(t)
	ctx := context.Background()
	locker, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer locker.Rollback(ctx)
	var account int64
	if e = locker.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' FOR UPDATE", buyer).Scan(&account); e != nil {
		t.Fatal(e)
	}
	type result struct {
		p MembershipPurchase
		e error
	}
	done := make(chan result, 1)
	go func() {
		p, e := s.PurchaseMembership(ctx, buyer, group, "member72-db-clock", "merchant:7211")
		done <- result{p, e}
	}()
	membership72WaitAccount(t, s)
	var release time.Time
	if e = locker.QueryRow(ctx, "SELECT pg_catalog.clock_timestamp()").Scan(&release); e != nil {
		t.Fatal(e)
	}
	if e = locker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	got := <-done
	if got.e != nil || got.p.ExpiresAt.Before(release.Add(30*24*time.Hour)) {
		t.Fatal("membership expiry used transaction-start or host clock before lock wait", release, got.p.ExpiresAt, got.e)
	}
}
