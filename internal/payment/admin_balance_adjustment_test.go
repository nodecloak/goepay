package payment

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// a3 F4: admin 后台加款/后台扣款 as a funds-authorised, idempotent, immutable
// journal; a debit can never drive the wallet below zero.
func TestAdminAdjustBalanceCreditDebitImmutableAndBounded(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 701, Name: "admin-adjust", APIKey: "adjust-key-long"}); e != nil {
		t.Fatal(e)
	}

	// 后台加款 500.
	credit, e := s.AdminAdjustBalance(ctx, 701, "credit", "cr-1", "补偿活动差额", 500, "admin:1")
	if e != nil {
		t.Fatal(e)
	}
	if credit.Direction != "credit" || credit.Amount != 500 {
		t.Fatalf("credit %+v", credit)
	}
	if b, _ := s.Balance(ctx, 701); b.Available != 500 {
		t.Fatalf("after credit %+v", b)
	}

	// Idempotent replay posts nothing new and returns the same row.
	replay, e := s.AdminAdjustBalance(ctx, 701, "credit", "cr-1", "补偿活动差额", 500, "admin:1")
	if e != nil || replay.ID != credit.ID {
		t.Fatalf("replay id=%d want %d err=%v", replay.ID, credit.ID, e)
	}
	if b, _ := s.Balance(ctx, 701); b.Available != 500 {
		t.Fatalf("replay double-posted %+v", b)
	}

	// Same external_no with different amount/direction is a hard conflict.
	if _, e = s.AdminAdjustBalance(ctx, 701, "credit", "cr-1", "x", 600, "admin:1"); !errors.Is(e, ErrConflict) {
		t.Fatalf("conflict amount %v", e)
	}
	if _, e = s.AdminAdjustBalance(ctx, 701, "debit", "cr-1", "x", 500, "admin:1"); !errors.Is(e, ErrConflict) {
		t.Fatalf("conflict direction %v", e)
	}

	// 后台扣款 300 within balance.
	if _, e = s.AdminAdjustBalance(ctx, 701, "debit", "db-1", "错误入账回收", 300, "admin:1"); e != nil {
		t.Fatal(e)
	}
	if b, _ := s.Balance(ctx, 701); b.Available != 200 {
		t.Fatalf("after debit %+v", b)
	}

	// A debit beyond the balance is rejected; balance unchanged, no row left.
	if _, e = s.AdminAdjustBalance(ctx, 701, "debit", "db-over", "超额", 201, "admin:1"); !errors.Is(e, ErrInsufficient) {
		t.Fatalf("over-debit %v", e)
	}
	if b, _ := s.Balance(ctx, 701); b.Available != 200 {
		t.Fatalf("balance moved on failed debit %+v", b)
	}
	var leftover int
	s.pool.QueryRow(ctx, "SELECT count(*) FROM admin_balance_adjustments WHERE external_no='db-over'").Scan(&leftover)
	if leftover != 0 {
		t.Fatalf("failed debit left an audit row: %d", leftover)
	}

	// Validation.
	for _, bad := range []struct {
		dir, ext, reason string
		amt              int64
	}{
		{"", "e", "r", 1}, {"credit", "", "r", 1}, {"credit", "e", "", 1}, {"credit", "e2", "r", 0}, {"debit", "e3", "r", -1},
	} {
		if _, e = s.AdminAdjustBalance(ctx, 701, bad.dir, bad.ext, bad.reason, bad.amt, "admin:1"); !errors.Is(e, ErrInvalid) {
			t.Fatalf("bad input %+v accepted (%v)", bad, e)
		}
	}

	// Concurrent identical replays post exactly once.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.AdminAdjustBalance(ctx, 701, "credit", "cr-conc", "并发补偿", 100, "admin:1"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if b, _ := s.Balance(ctx, 701); b.Available != 300 {
		t.Fatalf("concurrent credit double-posted %+v", b)
	}

	// The ledger journals are immutable 'admin-adjust:*' entries with the
	// original labels. Distinct posting journals: credit cr-1 and credit cr-conc
	// (the replay and the 8 concurrent dupes post nothing new), and debit db-1.
	var credits, debits int
	s.pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE description='后台加款'),count(*) FILTER(WHERE description='后台扣款') FROM journals WHERE event_key LIKE 'admin-adjust:%'").Scan(&credits, &debits)
	if credits != 2 || debits != 1 {
		t.Fatalf("journals credits=%d debits=%d", credits, debits)
	}
	if AdminAdjustRecordLabel(true) != "后台加款" || AdminAdjustRecordLabel(false) != "后台扣款" {
		t.Fatal("record label mapping wrong")
	}
}

// A disabled or unknown merchant cannot be adjusted.
func TestAdminAdjustBalanceRejectsUnknownMerchant(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, e := s.AdminAdjustBalance(ctx, 999999, "credit", "x", "r", 1, "admin:1"); !errors.Is(e, ErrNotFound) {
		t.Fatalf("unknown merchant %v", e)
	}
}
