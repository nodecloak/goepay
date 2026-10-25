package payment

import (
	"context"
	"errors"
	"testing"
)

func TestMembershipReferral72PolicySeparateDefaultOffCASAndInheritance(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	v, e := s.MembershipReferralPolicy(ctx, "global", 0, referralPolicyAdmin)
	if e != nil || v.Revision != 0 || v.Enabled != nil || v.RateBPS != nil {
		t.Fatal("new membership policy must default off without invented revision", v, e)
	}
	yes, rate := true, int64(2500)
	v, e = s.SetMembershipReferralPolicy(ctx, "global", 0, 0, MembershipReferralPolicyInput{Enabled: &yes, RateBPS: &rate}, referralPolicyAdmin)
	if e != nil || v.Revision != 1 || v.RateBPS == nil || *v.RateBPS != 2500 {
		t.Fatal(v, e)
	}
	if _, e = s.SetMembershipReferralPolicy(ctx, "global", 0, 0, MembershipReferralPolicyInput{Enabled: &yes, RateBPS: &rate}, referralPolicyAdmin); !errors.Is(e, ErrConflict) {
		t.Fatal("stale policy CAS accepted", e)
	}
	if _, e = s.SetMembershipReferralPolicy(ctx, "global", 0, 1, MembershipReferralPolicyInput{}, referralPolicyAdmin); !errors.Is(e, ErrInvalid) {
		t.Fatal("global null policy accepted", e)
	}
	bad := int64(10001)
	if _, e = s.SetMembershipReferralPolicy(ctx, "global", 0, 1, MembershipReferralPolicyInput{Enabled: &yes, RateBPS: &bad}, referralPolicyAdmin); !errors.Is(e, ErrInvalid) {
		t.Fatal("unfunded rate accepted", e)
	}
	var ordinary int64
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM referral_policies").Scan(&ordinary); e != nil || ordinary != 0 {
		t.Fatal("membership policy contaminated ordinary order policy", ordinary, e)
	}
}
