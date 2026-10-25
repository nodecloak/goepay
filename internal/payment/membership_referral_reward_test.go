package payment

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func membership72RewardFixture(t *testing.T) (*Store, int64, int64, int64) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	promoter, buyer := int64(7210), int64(7211)
	referralBoundMerchants(t, s, promoter, buyer)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: buyer, OutTradeNo: "membership72-fund", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "membership72-balance-receipt"); e != nil {
		t.Fatal(e)
	}
	yes, rate := true, int64(2500)
	if _, e = s.SetMembershipReferralPolicy(ctx, "global", 0, 0, MembershipReferralPolicyInput{Enabled: &yes, RateBPS: &rate}, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	return s, promoter, buyer, membership72Group(t, s, 300)
}

func TestMembershipReferral72RewardUsesOnlyOriginalPurchaseAndReplaysOriginalPolicy(t *testing.T) {
	s, promoter, buyer, group := membership72RewardFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	ids := make(chan int64, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := s.PurchaseMembership(ctx, buyer, group, "member72-reward-once", "merchant:7211")
			if e != nil {
				t.Error(e)
				return
			}
			ids <- p.ID
		}()
	}
	wg.Wait()
	close(ids)
	var id int64
	for got := range ids {
		if id != 0 && id != got {
			t.Fatal("duplicated purchase")
		}
		id = got
	}
	var price, reward, income, awards, ordinary, fees int64
	e := s.pool.QueryRow(ctx, `SELECT s.price,s.reward_amount,a.balance,(SELECT count(*) FROM membership_referral_rewards),(SELECT count(*) FROM referral_rewards),(SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees') FROM membership_referral_sources s JOIN accounts a ON a.id=s.income_account_id WHERE s.purchase_id=$1`, id).Scan(&price, &reward, &income, &awards, &ordinary, &fees)
	if e != nil || price != 300 || reward != 75 || income != 225 || awards != 1 || ordinary != 0 || fees != 0 {
		t.Fatal("membership reward borrowed ordinary fees or lacked original income", price, reward, income, awards, ordinary, fees, e)
	}
	b, e := s.Balance(ctx, promoter)
	if e != nil || b.Available != 75 {
		t.Fatal("reward not once", b, e)
	}
	no, rate := false, int64(10000)
	if _, e = s.SetMembershipReferralPolicy(ctx, "global", 0, 1, MembershipReferralPolicyInput{Enabled: &no, RateBPS: &rate}, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	old, e := s.PurchaseMembership(ctx, buyer, group, "member72-reward-once", "merchant:7211")
	if e != nil || old.ID != id {
		t.Fatal("original purchase replay repriced", old, e)
	}
	if _, e = s.PurchaseMembership(ctx, buyer, group, "member72-next-disabled", "merchant:7211"); e != nil {
		t.Fatal(e)
	}
	b, e = s.Balance(ctx, promoter)
	if e != nil || b.Available != 75 {
		t.Fatal("disabled new policy paid or old policy paid again", b, e)
	}
	other := membership72Group(t, s, 0)
	if _, e = s.PurchaseMembership(ctx, buyer, other, "member72-reward-once", "merchant:7211"); !errors.Is(e, ErrConflict) {
		t.Fatal("changed replay group accepted", e)
	}
	r, e := s.CheckLedger(ctx)
	if e != nil || r.InvalidJournals != 0 || r.InvalidAccounts != 0 || r.InvalidReferralTerms != 0 || r.InvalidReferralRewards != 0 {
		t.Fatal("legitimate membership lifecycle failed ledger check", r, e)
	}
}

func TestMembershipReferral72CheckerRejectsForgedSource(t *testing.T) {
	s, _, buyer, group := membership72RewardFixture(t)
	ctx := context.Background()
	p, e := s.PurchaseMembership(ctx, buyer, group, "member72-tampered-source", "merchant:7211")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE membership_referral_sources DISABLE TRIGGER membership_referral_source_immutable;UPDATE membership_referral_sources SET proof_hash=decode(repeat('01',32),'hex');ALTER TABLE membership_referral_sources ENABLE TRIGGER membership_referral_source_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PurchaseMembership(ctx, buyer, group, p.ExternalNo, "merchant:7211"); !errors.Is(e, ErrConflict) {
		t.Fatal("tampered source replay accepted", e)
	}
	r, e := s.CheckLedger(ctx)
	if e != nil || r.InvalidReferralTerms == 0 {
		t.Fatal("checker accepted forged membership source", r, e)
	}
}

func TestMembershipReferral72SourceVersionCannotBeReinterpreted(t *testing.T) {
	s, _, buyer, group := membership72RewardFixture(t)
	ctx := context.Background()
	p, e := s.PurchaseMembership(ctx, buyer, group, "member72-version-proof", "merchant:7211")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE membership_referral_sources DROP CONSTRAINT membership_referral_sources_source_version_check;ALTER TABLE membership_referral_sources DISABLE TRIGGER membership_referral_source_immutable;UPDATE membership_referral_sources SET source_version=2;ALTER TABLE membership_referral_sources ENABLE TRIGGER membership_referral_source_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PurchaseMembership(ctx, buyer, group, p.ExternalNo, "merchant:7211"); !errors.Is(e, ErrConflict) {
		t.Fatal("source version reinterpreted", e)
	}
}
