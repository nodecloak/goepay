package payment

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestReferralGross69IntegerMathBudgetAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		amount, fee, rate, want int64
		basis                   string
	}{
		{100, 10, 5000, 10, "gross"}, {100, 10, 500, 5, "gross"}, {100, 10, 50, 1, "gross"}, {100, 10, 49, 0, "gross"},
		{100, 0, 10000, 0, "gross"}, {100, 10, 5000, 5, "fee"}, {math.MaxInt64, math.MaxInt64, 10000, math.MaxInt64, "gross"},
		{math.MaxInt64, math.MaxInt64, 5000, 4611686018427387904, "gross"}, {math.MaxInt64, 1, 10000, 1, "gross"},
	} {
		got, e := calculateReferralPolicyReward(tc.amount, tc.fee, tc.rate, tc.basis)
		if e != nil || got != tc.want {
			t.Fatal(tc, got, e)
		}
	}
	for _, tc := range []struct {
		amount, fee, rate int64
		basis             string
	}{{-1, 1, 1, "gross"}, {100, -1, 1, "gross"}, {100, 10, -1, "gross"}, {100, 10, 10001, "gross"}, {100, 10, 1, "profit"}, {100, 10, 1, ""}} {
		if _, e := calculateReferralPolicyReward(tc.amount, tc.fee, tc.rate, tc.basis); e == nil {
			t.Fatal("invalid basis/math accepted", tc)
		}
	}
}

func TestReferralGross69CappedOriginalTermsAndImmutableFeeProof(t *testing.T) {
	s, promoter, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	old := referralCreateTestOrder(t, s, mid, 100, "legacy-fee-proof69")
	read := func(o Order) referralOrderTerms {
		t.Helper()
		tx, e := s.pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		term, e := s.readReferralTerms(ctx, tx, o)
		if e != nil {
			t.Fatal(e)
		}
		return term
	}
	oldTerms := read(old)
	enabled, rate := true, int64(5000)
	input := referralTestPolicy(&enabled, &rate)
	input.Basis = "gross"
	gross, e := s.SetReferralPolicy(ctx, "global", 0, 1, input, referralPolicyAdmin)
	if e != nil || gross.Basis != "gross" {
		t.Fatal("explicit gross policy rejected", gross, e)
	}
	capped := referralCreateTestOrder(t, s, mid, 100, "gross-capped69")
	terms := read(capped)
	if terms.RewardAmount != 10 || terms.PlatformFee != 10 || terms.PolicyVersions[0] != gross.ID {
		t.Fatal("gross did not retain original fee cap", terms)
	}
	if got := read(old); got.RewardAmount != 5 || string(got.ProofHash) != string(oldTerms.ProofHash) {
		t.Fatal("new gross policy changed old v1 fee terms", got)
	}
	rate = 500
	input.RateBPS = &rate
	if _, e = s.SetReferralPolicy(ctx, "global", 0, 2, input, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	smaller := referralCreateTestOrder(t, s, mid, 100, "gross-not-capped69")
	if got := read(smaller); got.RewardAmount != 5 {
		t.Fatal("gross not calculated from order amount", got)
	}
	input.Basis = "fee"
	if _, e = s.SetReferralPolicy(ctx, "global", 0, 3, input, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	if got := read(capped); got.RewardAmount != 10 || string(got.ProofHash) != string(terms.ProofHash) {
		t.Fatal("head change rewrote gross terms", got)
	}
	referralPayTestOrder(t, s, capped)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if e := s.markPaid(ctx, capped.TradeNo, fmt.Sprintf("gross-repeat69:%d", i), true, PaidReceipt{RealMoney: capped.Amount, ChannelID: 7, ProviderTradeNo: "LOCAL_" + capped.TradeNo}); e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	b, e := s.Balance(ctx, promoter)
	if e != nil || b.Available != 10 {
		t.Fatal("gross reward duplicated or changed by policy head", b, e)
	}
	var fees, invalid int64
	if e = s.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees' AND scope_id=0").Scan(&fees); e != nil || fees != 0 {
		t.Fatal("gross spent beyond original platform fee", fees, e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM referral_reward_invariants WHERE valid IS NOT TRUE").Scan(&invalid); e != nil || invalid != 0 {
		t.Fatal("gross award invariants", invalid, e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	source, e := s.referralRefundSource(ctx, tx, capped.ID)
	if e != nil || source == nil || source.Amount != 10 || source.PlatformFee != 10 {
		t.Fatal("gross refund source changed", source, e)
	}
}

func TestReferralGross69BasisMergeAndZeroFee(t *testing.T) {
	s, promoter, mid, payerGroup := referralRewardFixture(t)
	ctx := context.Background()
	enabled, rate := true, int64(5000)
	input := referralTestPolicy(&enabled, &rate)
	input.Basis = "gross"
	if _, e := s.SetReferralPolicy(ctx, "global", 0, 1, input, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	check := func(no string, want int64) {
		t.Helper()
		o := referralCreateTestOrder(t, s, mid, 100, no)
		tx, e := s.pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		term, e := s.readReferralTerms(ctx, tx, o)
		if e != nil || term.RewardAmount != want {
			t.Fatal(no, term, e)
		}
	}
	check("gross-global69", 10)
	if _, e := s.SetReferralPolicy(ctx, "group", payerGroup, 0, referralTestPolicy(nil, nil), referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	check("payer-fee69", 5)
	var promoterGroup int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"gross69 promoter group","enabled":true,"rate_bps":"1000"}') RETURNING id`).Scan(&promoterGroup); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, "UPDATE merchants SET profile=jsonb_build_object('group_id',$2::bigint::text) WHERE id=$1", promoter, promoterGroup); e != nil {
		t.Fatal(e)
	}
	group := referralTestPolicy(nil, nil)
	group.Basis = "gross"
	if _, e := s.SetReferralPolicy(ctx, "group", promoterGroup, 0, group, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	check("promoter-gross69", 10)
	if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{rate_bps}','"0"') WHERE resource='groups' AND id=$1`, payerGroup); e != nil {
		t.Fatal(e)
	}
	check("zero-fee-gross69", 0)
}

func TestReferralGross69UnknownRefundRestoreAndPartialClawback(t *testing.T) {
	s, promoter, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	enabled, rate := true, int64(5000)
	input := referralTestPolicy(&enabled, &rate)
	input.Basis = "gross"
	if _, e := s.SetReferralPolicy(ctx, "global", 0, 1, input, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	o := referralCreateTestOrder(t, s, mid, 100, "gross-refund69")
	referralPayTestOrder(t, s, o)
	r, e := s.ReserveRefund(ctx, mid, o.OutTradeNo, "gross-first69", 98)
	if e != nil || r.FeeAmount != 8 {
		t.Fatal(r, e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "unknown"); e != nil {
		t.Fatal(e)
	}
	b, e := s.Balance(ctx, promoter)
	if e != nil || b.Available != 0 {
		t.Fatal("unknown did not escrow original gross reward", b, e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	b, e = s.Balance(ctx, promoter)
	if e != nil || b.Available != 10 {
		t.Fatal("failed did not restore exact original gross reward", b, e)
	}
	partial, e := s.ReserveRefund(ctx, mid, o.OutTradeNo, "gross-partial69", 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, partial.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, partial.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	b, e = s.Balance(ctx, promoter)
	if e != nil || b.Available != 0 {
		t.Fatal("partial refund did not void full original gross reward", b, e)
	}
	remaining, e := s.ReserveRefund(ctx, mid, o.OutTradeNo, "gross-remaining69", 99)
	if e != nil || remaining.FeeAmount != 10 {
		t.Fatal(remaining, e)
	}
	if e = s.ResolveRefund(ctx, remaining.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	ledger, e := s.CheckLedger(ctx)
	if e != nil || ledger.InvalidReferralTerms != 0 || ledger.InvalidReferralRewards != 0 || ledger.InvalidReferralClawbacks != 0 || ledger.InvalidJournals != 0 || ledger.InvalidAccounts != 0 {
		t.Fatal(ledger, e)
	}
}
