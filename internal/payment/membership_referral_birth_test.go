package payment

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func membership72Buyer(t *testing.T, s *Store, mid, amount int64) {
	t.Helper()
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: fmt.Sprint("membership72-", mid), APIKey: "membership72-local-test-key"}); e != nil {
		t.Fatal(e)
	}
	if amount > 0 {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "membership72-original-balance", Amount: amount})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.MarkPaid(ctx, o.TradeNo, "membership72-original-credit"); e != nil {
			t.Fatal(e)
		}
	}
}

func membership72Group(t *testing.T, s *Store, price int64) int64 {
	t.Helper()
	var id int64
	e := s.pool.QueryRow(context.Background(), `INSERT INTO backoffice_records(resource,body) VALUES('groups',jsonb_build_object('name',$1::text,'enabled',true,'price_cents',$2::text,'duration_days','30')) RETURNING id`, fmt.Sprint("membership72-price-", price), fmt.Sprint(price)).Scan(&id)
	if e != nil {
		t.Fatal(e)
	}
	return id
}

func TestMembershipReferral72OriginalPurchaseIncomeIsolatedAndOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	membership72Buyer(t, s, 7201, 1000)
	group := membership72Group(t, s, 300)
	var clearingBefore int64
	if e := s.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0").Scan(&clearingBefore); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan MembershipPurchase, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := s.PurchaseMembership(ctx, 7201, group, "membership72-once", "merchant:7201")
			if e != nil {
				t.Error(e)
				return
			}
			results <- p
		}()
	}
	wg.Wait()
	close(results)
	var purchase int64
	for p := range results {
		if purchase != 0 && purchase != p.ID {
			t.Fatal("duplicate purchase IDs")
		}
		purchase = p.ID
	}
	var version, income, price, reward, sources, awards, clearingAfter int64
	var reason string
	e := s.pool.QueryRow(ctx, `SELECT p.source_version,a.balance,s.price,s.reward_amount,s.reason,(SELECT count(*) FROM membership_referral_sources),(SELECT count(*) FROM membership_referral_rewards),(SELECT balance FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0) FROM membership_purchases p JOIN membership_referral_sources s ON s.purchase_id=p.id JOIN accounts a ON a.id=s.income_account_id AND a.owner_id=0 AND a.kind='membership_income' AND a.scope_id=p.id WHERE p.id=$1`, purchase).Scan(&version, &income, &price, &reward, &reason, &sources, &awards, &clearingAfter)
	if e != nil || version != 1 || income != 300 || price != 300 || reward != 0 || reason != "no_relation" || sources != 1 || awards != 0 || clearingAfter != clearingBefore {
		t.Fatal("purchase income was not original isolated once-only source", version, income, price, reward, reason, sources, awards, clearingBefore, clearingAfter, e)
	}
	b, e := s.Balance(ctx, 7201)
	if e != nil || b.Available != 700 {
		t.Fatal("buyer charged more than once", b, e)
	}
}

func TestMembershipReferral72FreePurchaseHasExplicitZeroSourceWithoutMoney(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	membership72Buyer(t, s, 7202, 0)
	group := membership72Group(t, s, 0)
	p, e := s.PurchaseMembership(ctx, 7202, group, "membership72-free", "merchant:7202")
	if e != nil {
		t.Fatal(e)
	}
	var sources, accounts, journals int64
	e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM membership_referral_sources WHERE purchase_id=$1 AND price=0 AND reward_amount=0 AND income_account_id IS NULL AND purchase_journal_id IS NULL),(SELECT count(*) FROM accounts WHERE kind='membership_income'),(SELECT count(*) FROM journals)`, p.ID).Scan(&sources, &accounts, &journals)
	if e != nil || sources != 1 || accounts != 0 || journals != 0 {
		t.Fatal("free membership invented income or lacked explicit source", sources, accounts, journals, e)
	}
}
