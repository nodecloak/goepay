package payment

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestMembershipReferral72ExactMathAndBudgetBounds(t *testing.T) {
	for _, v := range [][3]int64{{0, 10000, 0}, {1, 5000, 1}, {1, 4999, 0}, {3, 5000, 2}, {300, 2500, 75}, {math.MaxInt64, 10000, math.MaxInt64}} {
		got, e := membershipReferralReward(v[0], v[1])
		if e != nil || got != v[2] || got > v[0] {
			t.Fatal(v, got, e)
		}
	}
	for _, v := range [][2]int64{{-1, 1}, {1, -1}, {1, 10001}} {
		if _, e := membershipReferralReward(v[0], v[1]); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid reward basis accepted", v, e)
		}
	}
}

func TestMembershipReferral72SQLNullAndNegativeBudgetCannotBypassChecks(t *testing.T) {
	s, _, buyer, group := membership72RewardFixture(t)
	ctx := context.Background()
	p, e := s.PurchaseMembership(ctx, buyer, group, "member72-sql-checks", "merchant:7211")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "ALTER TABLE membership_referral_sources DISABLE TRIGGER membership_referral_source_immutable"); e != nil {
		t.Fatal(e)
	}
	defer s.pool.Exec(ctx, "ALTER TABLE membership_referral_sources ENABLE TRIGGER membership_referral_source_immutable")
	for _, set := range []string{"income_account_id=NULL", "purchase_journal_id=NULL", "reward_amount=-1", "reward_amount=price+1", "promoter_id=NULL"} {
		_, e = s.pool.Exec(ctx, "UPDATE membership_referral_sources SET "+set+" WHERE purchase_id=$1", p.ID)
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "23514" {
			t.Fatal("SQL check accepted invalid source", set, e)
		}
	}
	_, e = s.pool.Exec(ctx, "UPDATE accounts SET balance=-1 WHERE kind='membership_income' AND scope_id=$1", p.ID)
	var pe *pgconn.PgError
	if !errors.As(e, &pe) || pe.ConstraintName != "membership_income_nonnegative" {
		t.Fatal("membership income inherited clearing negative exception", e)
	}
	if _, e = s.pool.Exec(ctx, "ALTER TABLE membership_referral_policies DISABLE TRIGGER membership_referral_policy_immutable"); e != nil {
		t.Fatal(e)
	}
	defer s.pool.Exec(ctx, "ALTER TABLE membership_referral_policies ENABLE TRIGGER membership_referral_policy_immutable")
	for _, set := range []string{"enabled=NULL", "rate_bps=NULL"} {
		_, e = s.pool.Exec(ctx, "UPDATE membership_referral_policies SET "+set+" WHERE scope_kind='global'")
		if !errors.As(e, &pe) || pe.Code != "23514" {
			t.Fatal("global policy null bypassed check", set, e)
		}
	}
}

func TestMembershipReferral72MissingSourceAndVersionDowngradeNeverBecomeLegacy(t *testing.T) {
	for _, damage := range []string{"remove-source", "downgrade"} {
		t.Run(damage, func(t *testing.T) {
			s, _, buyer, group := membership72RewardFixture(t)
			ctx := context.Background()
			p, e := s.PurchaseMembership(ctx, buyer, group, "member72-missing", "merchant:7211")
			if e != nil {
				t.Fatal(e)
			}
			if damage == "remove-source" {
				_, e = s.pool.Exec(ctx, `ALTER TABLE membership_referral_rewards DROP CONSTRAINT membership_referral_rewards_purchase_id_fkey;ALTER TABLE membership_referral_events DROP CONSTRAINT membership_referral_events_purchase_id_fkey;ALTER TABLE membership_referral_sources DISABLE TRIGGER membership_referral_source_immutable;DELETE FROM membership_referral_sources;ALTER TABLE membership_referral_sources ENABLE TRIGGER membership_referral_source_immutable`)
			} else {
				_, e = s.pool.Exec(ctx, `ALTER TABLE membership_purchases DISABLE TRIGGER membership_purchases_immutable;UPDATE membership_purchases SET source_version=0;ALTER TABLE membership_purchases ENABLE TRIGGER membership_purchases_immutable`)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.PurchaseMembership(ctx, buyer, group, p.ExternalNo, "merchant:7211"); !errors.Is(e, ErrConflict) {
				t.Fatal("new purchase downgraded to legacy", e)
			}
			r, e := s.CheckLedger(ctx)
			if e != nil || r.InvalidReferralTerms == 0 || r.InvalidAccounts == 0 {
				t.Fatal("missing source or new legacy downgrade was invisible", r, e)
			}
		})
	}
}

func TestMembershipReferral72ExtraAuthenticatedEventCannotReplaceAnAward(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	membership72Buyer(t, s, 7230, 0)
	group := membership72Group(t, s, 0)
	p, e := s.PurchaseMembership(ctx, 7230, group, "member72-extra-event", "merchant:7230")
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.readMembershipReferralSource(ctx, s.pool, p)
	if e != nil {
		t.Fatal(e)
	}
	key := fmt.Sprintf("membership-referral:%d:award", p.ID)
	proof, e := s.membershipReferralDigest("event", []any{p.ID, "award", key, p.Actor, hex.EncodeToString(v.ProofHash)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "INSERT INTO membership_referral_events(purchase_id,operation,event_key,actor,subject_hash,proof_hash) VALUES($1,'award',$2,$3,$4,$5)", p.ID, key, p.Actor, v.ProofHash, proof); e != nil {
		t.Fatal(e)
	}
	r, e := s.CheckLedger(ctx)
	if e != nil || r.InvalidReferralTerms == 0 {
		t.Fatal("valid HMAC event without actual award accepted", r, e)
	}
}

func TestMembershipReferral72PolicyInheritanceAndExplicitFalseZero(t *testing.T) {
	s, promoter, buyer, target := membership72RewardFixture(t)
	ctx := context.Background()
	payerGroup := membership72Group(t, s, 10)
	promoterGroup := membership72Group(t, s, 20)
	for _, v := range [][2]int64{{buyer, payerGroup}, {promoter, promoterGroup}} {
		if _, e := s.pool.Exec(ctx, "UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($2::text)) WHERE id=$1", v[0], fmt.Sprint(v[1])); e != nil {
			t.Fatal(e)
		}
	}
	yes, global, payer, promo := true, int64(1000), int64(2000), int64(3000)
	if _, e := s.SetMembershipReferralPolicy(ctx, "global", 0, 1, MembershipReferralPolicyInput{Enabled: &yes, RateBPS: &global}, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SetMembershipReferralPolicy(ctx, "group", payerGroup, 0, MembershipReferralPolicyInput{RateBPS: &payer}, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SetMembershipReferralPolicy(ctx, "group", promoterGroup, 0, MembershipReferralPolicyInput{RateBPS: &promo}, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	p, e := s.PurchaseMembership(ctx, buyer, target, "member72-inheritance", "merchant:7211")
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.readMembershipReferralSource(ctx, s.pool, p)
	if e != nil || v.Reward != 90 || v.RateBPS != 3000 || v.PayerGroupID != payerGroup || v.PromoterGroupID != promoterGroup {
		t.Fatal("policy chain not fixed in original source", v, e)
	}
	no, zero := false, int64(0)
	if _, e = s.SetMembershipReferralPolicy(ctx, "group", promoterGroup, 1, MembershipReferralPolicyInput{Enabled: &no, RateBPS: &zero}, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	next, e := s.PurchaseMembership(ctx, buyer, target, "member72-explicit-false", "merchant:7211")
	if e != nil {
		t.Fatal(e)
	}
	v, e = s.readMembershipReferralSource(ctx, s.pool, next)
	if e != nil || v.Reward != 0 || v.Reason != "disabled" || v.RateBPS != 0 {
		t.Fatal("false or zero was mistaken for inheritance", v, e)
	}
	if _, e = s.PurchaseMembership(ctx, buyer, target, p.ExternalNo, "merchant:7211"); e != nil {
		t.Fatal("old source depended on current policy/group", e)
	}
}
