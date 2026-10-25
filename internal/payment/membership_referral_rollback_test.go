package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func membership72NoPartialPurchase(t *testing.T, s *Store, buyer, promoter int64) {
	t.Helper()
	ctx := context.Background()
	var purchases, sources, rewards, events, incomes, memberships int64
	e := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM membership_purchases),(SELECT count(*) FROM membership_referral_sources),(SELECT count(*) FROM membership_referral_rewards),(SELECT count(*) FROM membership_referral_events),(SELECT count(*) FROM accounts WHERE kind='membership_income'),(SELECT count(*) FROM memberships)`).Scan(&purchases, &sources, &rewards, &events, &incomes, &memberships)
	if e != nil || purchases+sources+rewards+events+incomes+memberships != 0 {
		t.Fatal("failed purchase left partial state", purchases, sources, rewards, events, incomes, memberships, e)
	}
	b, e := s.Balance(ctx, buyer)
	if e != nil || b.Available != 1000 {
		t.Fatal("failed purchase charged original buyer", b, e)
	}
	b, e = s.Balance(ctx, promoter)
	if e != nil || b.Available != 0 {
		t.Fatal("failed purchase credited promoter", b, e)
	}
}

func TestMembershipReferral72SourceAndAwardAuditFaultRollBackWholePurchase(t *testing.T) {
	for _, op := range []string{"source", "award"} {
		t.Run(op, func(t *testing.T) {
			s, promoter, buyer, group := membership72RewardFixture(t)
			ctx := context.Background()
			if _, e := s.pool.Exec(ctx, `CREATE FUNCTION membership72_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation=TG_ARGV[0] THEN RAISE EXCEPTION 'synthetic membership audit failure';END IF;RETURN NEW;END $$;CREATE TRIGGER membership72_audit_fault BEFORE INSERT ON membership_referral_events FOR EACH ROW EXECUTE FUNCTION membership72_audit_fault('`+op+`')`); e != nil {
				t.Fatal(e)
			}
			if _, e := s.PurchaseMembership(ctx, buyer, group, "member72-audit-fault", "merchant:7211"); e == nil {
				t.Fatal("audit fault ignored")
			}
			membership72NoPartialPurchase(t, s, buyer, promoter)
		})
	}
}

func TestMembershipReferral72OriginalFundsGuardCannotBeSkippedOrReusedForPolicy(t *testing.T) {
	s, promoter, buyer, group := membership72RewardFixture(t)
	ctx := context.Background()
	guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return ErrState })
	if _, e := s.PurchaseMembership(guarded, buyer, group, "member72-denied-grant", "merchant:7211"); !errors.Is(e, ErrState) {
		t.Fatal("purchase skipped original funds guard", e)
	}
	membership72NoPartialPurchase(t, s, buyer, promoter)
	yes, rate := true, int64(10000)
	ordinary := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, _ string) error {
		if kind == "referral-policy" {
			return nil
		}
		return ErrState
	})
	if _, e := s.SetMembershipReferralPolicy(ordinary, "global", 0, 1, MembershipReferralPolicyInput{Enabled: &yes, RateBPS: &rate}, referralPolicyAdmin); !errors.Is(e, ErrState) {
		t.Fatal("ordinary policy grant authorized membership reward", e)
	}
	op := referralPolicyAdmin
	op.Class = "operator"
	if _, e := s.SetMembershipReferralPolicy(ctx, "global", 0, 1, MembershipReferralPolicyInput{Enabled: &yes, RateBPS: &rate}, op); !errors.Is(e, ErrInvalid) {
		t.Fatal("operator changed financial membership policy", e)
	}
}

func TestMembershipReferral72PolicyAuditFaultRollsBackVersionAndHead(t *testing.T) {
	s, _, _, _ := membership72RewardFixture(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION membership72_policy_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic membership policy audit failure';END $$;CREATE TRIGGER membership72_policy_fault BEFORE INSERT ON membership_referral_policy_audits FOR EACH ROW EXECUTE FUNCTION membership72_policy_fault()`); e != nil {
		t.Fatal(e)
	}
	yes, rate := true, int64(10000)
	if _, e := s.SetMembershipReferralPolicy(ctx, "global", 0, 1, MembershipReferralPolicyInput{Enabled: &yes, RateBPS: &rate}, referralPolicyAdmin); e == nil {
		t.Fatal("policy audit fault ignored")
	}
	v, e := s.MembershipReferralPolicy(ctx, "global", 0, referralPolicyAdmin)
	if e != nil || v.Revision != 1 || v.RateBPS == nil || *v.RateBPS != 2500 {
		t.Fatal("audit failure changed published policy", v, e)
	}
	var count int64
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM membership_referral_policies").Scan(&count); e != nil || count != 1 {
		t.Fatal("uncommitted policy version survived", count, e)
	}
}

func TestMembershipReferral72InsufficientBuyerCannotBorrowAnotherPurchaseIncome(t *testing.T) {
	s, promoter, buyer, group := membership72RewardFixture(t)
	ctx := context.Background()
	membership72Buyer(t, s, 7240, 3000)
	if _, e := s.pool.Exec(ctx, "UPDATE backoffice_records SET body=jsonb_set(body,'{price_cents}','\"2000\"') WHERE id=$1", group); e != nil {
		t.Fatal(e)
	}
	other, e := s.PurchaseMembership(ctx, 7240, group, "member72-other-income", "merchant:7240")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PurchaseMembership(ctx, buyer, group, "member72-insufficient-original-buyer", "merchant:7211"); !errors.Is(e, ErrInsufficient) {
		t.Fatal("another purchase income funded this purchase", e)
	}
	var income, purchases int64
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT balance FROM accounts WHERE kind='membership_income' AND scope_id=$1),(SELECT count(*) FROM membership_purchases)", other.ID).Scan(&income, &purchases); e != nil || income != 2000 || purchases != 1 {
		t.Fatal("failed purchase spent another original budget", income, purchases, e)
	}
	b, e := s.Balance(ctx, promoter)
	if e != nil || b.Available != 0 {
		t.Fatal("unfunded reward credited", b, e)
	}
}
