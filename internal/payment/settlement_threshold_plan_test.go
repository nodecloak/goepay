package payment

import (
	"testing"
	"time"
)

func thresholdPlanning74() (settlementPlanAuthorizationRecord, []settlementThresholdCandidate, time.Time) {
	at := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)
	p := SettlementThresholdPolicy{ThresholdCents: 100, FeeRateBPS: 300, FeeMaximumCents: 25}
	a := settlementPlanAuthorizationRecord{View: SettlementPlanAuthorizationView{ID: 1, PlanID: 1, PlanRevision: 1, SettlementPlanAuthorizationInput: SettlementPlanAuthorizationInput{ValidFrom: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), MaxItemGross: 1000, MaxRunGross: 3000, MaxDayGross: 3000, MaxDayItems: 100}}, Scope: settlementPlanScope{Threshold: &settlementThresholdConfig{Enabled: true, ChannelID: 7, Policy: p, SettleType: "0"}}}
	candidates := []settlementThresholdCandidate{}
	for mid := int64(1); mid <= 6; mid++ {
		a.Scope.Merchants = append(a.Scope.Merchants, settlementScopeMerchant{MerchantID: mid, RecipientConfigID: 200 + mid, Threshold: &settlementThresholdMerchantPolicy{MerchantID: mid, MerchantRevision: 1, Policy: p}})
		candidates = append(candidates, settlementThresholdCandidate{MerchantID: mid, LedgerAvailable: 1000, Eligible: 1000})
	}
	a.Scope.Merchants[5].Threshold.GroupDisabled = true
	zero, lower := int64(0), int64(125)
	a.Scope.Merchants[3].Threshold.FeeOverrideBPS = &zero
	a.Scope.Merchants[3].Threshold.Policy.FeeRateBPS = 0
	a.Scope.Merchants[2].Threshold.FeeOverrideBPS = &lower
	a.Scope.Merchants[2].Threshold.Policy.FeeRateBPS = 125
	return a, candidates, at
}

func TestSettlementThreshold74PlansOriginalFiveInUIDOrderWithoutReplacingClosedGroup(t *testing.T) {
	a, candidates, at := thresholdPlanning74()
	items, after, count, e := planThresholdItems(a, candidates, []int64{101, 102, 103, 104, 105}, 0, 0, at)
	if e != nil || len(items) != 5 || after != 3000 || count != 3 {
		t.Fatal(items, after, count, e)
	}
	for i, v := range items {
		if v.MerchantID != int64(6-i) || v.ID != int64(101+i) || v.RequestID != 0 || v.Retained != 0 {
			t.Fatal("original proposal identity/order", v)
		}
	}
	if items[0].State != "skipped" || items[0].Reason != "group_closed" || items[0].Gross != 0 {
		t.Fatal("closed group was replaced or charged", items[0])
	}
	for i, want := range []struct{ fee, net int64 }{{25, 975}, {0, 1000}, {13, 987}} {
		v := items[i+1]
		if v.State != "created" || v.Gross != 1000 || v.Fee != want.fee || v.Net != want.net {
			t.Fatal("original group rate/global max", v, want)
		}
	}
	if items[4].State != "skipped" || items[4].Reason != "budget_exhausted" || items[4].Gross != 0 {
		t.Fatal("run quota was exceeded", items[4])
	}
}

func TestSettlementThreshold74PlansNeverClipWholeBalanceOrResetReducedAuthority(t *testing.T) {
	a, candidates, at := thresholdPlanning74()
	a.View.MaxRunGross = 2500
	a.View.MaxDayGross = 2500
	items, after, count, e := planThresholdItems(a, candidates, []int64{101, 102, 103, 104, 105}, 1000, 1, at)
	if e != nil || after != 2000 || count != 2 || items[1].Gross != 1000 {
		t.Fatal(items, after, count, e)
	}
	for _, v := range items[2:] {
		if v.Gross != 0 || v.Reason != "budget_exhausted" {
			t.Fatal("whole wallet was clipped to spare500 cents", v)
		}
	}
	a.View.MaxRunGross = 1000
	a.View.MaxDayGross = 1000
	items, after, count, e = planThresholdItems(a, candidates, []int64{101, 102, 103, 104, 105}, 2000, 2, at)
	if e != nil || after != 2000 || count != 2 {
		t.Fatal("new smaller authority rewrote original consumption", after, count, e)
	}
	for _, v := range items {
		if v.Gross != 0 {
			t.Fatal("new authority reset historical day budget", v)
		}
	}
	if _, _, _, e = planThresholdItems(a, candidates, []int64{101}, 0, 0, at); e == nil {
		t.Fatal("proposal IDs missing from original choice")
	}
	for i := range candidates {
		candidates[i].LedgerAvailable = 0
		candidates[i].Eligible = 0
	}
	if items, after, count, e = planThresholdItems(a, candidates, nil, 0, 0, at); e != nil || len(items) != 0 || after != 0 || count != 0 {
		t.Fatal("empty original choice created fabricated rows", items, after, count, e)
	}
}
