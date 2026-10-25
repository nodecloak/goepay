package payment

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestMembershipAndDepositsUseBalancedLedger(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 91, Name: "membership/deposit", APIKey: "test-key-long"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 91, OutTradeNo: "fund", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "business-fund"); e != nil {
		t.Fatal(e)
	}
	var groupID int64
	if e = s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"Premium","enabled":true,"price_cents":"300","duration_days":"30"}') RETURNING id`).Scan(&groupID); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.PurchaseMembership(ctx, 91, groupID, "purchase1", "merchant:91"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	b, _ := s.Balance(ctx, 91)
	if b.Available != 1700 {
		t.Fatalf("duplicate membership charged %+v", b)
	}
	if _, e = s.Deposit(ctx, 91, "hold", "hold1", 500, "merchant:91"); e != nil {
		t.Fatal(e)
	}
	release, e := s.Deposit(ctx, 91, "release", "release1", 200, "merchant:91")
	if e != nil {
		t.Fatal(e)
	}
	held, pending, e := s.DepositBalance(ctx, 91)
	if e != nil || held != 300 || pending != 200 {
		t.Fatalf("deposit hold%d pending%d %v", held, pending, e)
	}
	if _, e = s.Deposit(ctx, 91, "release", "over", 301, "merchant:91"); !errors.Is(e, ErrInsufficient) {
		t.Fatalf("over-release %v", e)
	}
	if _, e = s.ReviewDeposit(ctx, release.ID, false, "admin:1"); e != nil {
		t.Fatal(e)
	}
	held, pending, _ = s.DepositBalance(ctx, 91)
	if held != 500 || pending != 0 {
		t.Fatalf("reject hold%d pending%d", held, pending)
	}
	release, e = s.Deposit(ctx, 91, "release", "release2", 200, "merchant:91")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.ReviewDeposit(ctx, release.ID, true, "admin:1"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	b, _ = s.Balance(ctx, 91)
	held, pending, _ = s.DepositBalance(ctx, 91)
	if b.Available != 1400 || held != 300 || pending != 0 {
		t.Fatalf("final %+v held%d pending%d", b, held, pending)
	}
	var count int
	s.pool.QueryRow(ctx, "SELECT count(*) FROM membership_purchases").Scan(&count)
	if count != 1 {
		t.Fatalf("purchases %d", count)
	}
}
func TestMerchantConfigRevisionPreventsSecretRollback(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 92, Name: "key", APIKey: "test-key-original"}); e != nil {
		t.Fatal(e)
	}
	old, e := s.Merchant(ctx, 92)
	if e != nil {
		t.Fatal(e)
	}
	fresh := old
	fresh.APIKey = "new-key-never-overwrite"
	if e = s.ReplaceMerchantAtomic(ctx, fresh, []byte(`{}`), nil); e != nil {
		t.Fatal(e)
	}
	old.Name = "stale admin edit"
	if e = s.ReplaceMerchantAtomic(ctx, old, []byte(`{}`), nil); !errors.Is(e, ErrConflict) {
		t.Fatalf("stale overwrote key: %v", e)
	}
	m, e := s.Merchant(ctx, 92)
	if e != nil || m.APIKey != "new-key-never-overwrite" {
		t.Fatalf("key rollback %v", e)
	}
}
