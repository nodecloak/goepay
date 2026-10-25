package payment

import "time"

// planThresholdItems fixes every selected outcome before any reserve is posted.
// Historical consumption belongs to the plan/day and survives replacement of
// its finite authority, including replacement with a smaller allowance.
func planThresholdItems(a settlementPlanAuthorizationRecord, candidates []settlementThresholdCandidate, ids []int64, beforeGross, beforeItems int64, at time.Time) ([]SettlementPlanItemView, int64, int64, error) {
	bad := func() ([]SettlementPlanItemView, int64, int64, error) {
		return nil, beforeGross, beforeItems, ErrInvalid
	}
	if validateSettlementPlanAuthorization(a.View.SettlementPlanAuthorizationInput) != nil || settlementPlanAuthorityAt(a.View, at) != "" || a.Scope.Threshold == nil || !a.Scope.Threshold.Enabled || a.Scope.Threshold.Policy.validate() != nil || a.Scope.Threshold.ChannelID <= 0 || len(candidates) == 0 || len(candidates) > 100 || len(candidates) != len(a.Scope.Merchants) || beforeGross < 0 || beforeGross > 100000000000 || beforeItems < 0 || beforeItems > 100 || (beforeGross == 0) != (beforeItems == 0) {
		return bad()
	}
	for i, c := range candidates {
		m := a.Scope.Merchants[i]
		if c.MerchantID <= 0 || (i > 0 && candidates[i-1].MerchantID >= c.MerchantID) || c.MerchantID != m.MerchantID || c.LedgerAvailable < 0 || c.Eligible < 0 || c.Eligible > c.LedgerAvailable || m.RecipientConfigID <= 0 || m.Threshold == nil || m.Threshold.MerchantID != c.MerchantID || m.Threshold.Policy.validate() != nil || m.Threshold.Policy.ThresholdCents != a.Scope.Threshold.Policy.ThresholdCents || m.Threshold.Policy.FeeMaximumCents != a.Scope.Threshold.Policy.FeeMaximumCents {
			return bad()
		}
		rate := a.Scope.Threshold.Policy.FeeRateBPS
		if m.Threshold.FeeOverrideBPS != nil {
			rate = *m.Threshold.FeeOverrideBPS
		}
		if m.Threshold.Policy.FeeRateBPS != rate {
			return bad()
		}
	}
	selected := thresholdSelectedWallets(candidates, a.Scope.Threshold.Policy.ThresholdCents)
	if len(ids) != len(selected) {
		return bad()
	}
	for i, id := range ids {
		if id <= 0 || i > 0 && ids[i-1] >= id {
			return bad()
		}
	}
	items := make([]SettlementPlanItemView, 0, len(selected))
	afterGross, afterItems, runGross := beforeGross, beforeItems, int64(0)
	for i := len(candidates) - 1; i >= 0 && len(items) < len(selected); i-- {
		c, m := candidates[i], a.Scope.Merchants[i]
		if c.LedgerAvailable <= a.Scope.Threshold.Policy.ThresholdCents {
			continue
		}
		v := SettlementPlanItemView{ID: ids[len(items)], MerchantID: c.MerchantID, RecipientConfigID: m.RecipientConfigID, LedgerAvailable: c.LedgerAvailable, Eligible: c.Eligible, State: "skipped"}
		switch {
		case m.Threshold.GroupDisabled:
			v.Reason = "group_closed"
		case afterGross >= a.View.MaxDayGross || afterItems >= a.View.MaxDayItems || runGross >= a.View.MaxRunGross:
			v.Reason = "budget_exhausted"
		default:
			maximum := min(a.View.MaxItemGross, a.View.MaxRunGross-runGross, a.View.MaxDayGross-afterGross)
			quote, e := quoteThresholdSettlement(m.Threshold.Policy, c.LedgerAvailable, c.Eligible, maximum, at)
			if e != nil {
				v.Reason = "amount_policy"
			} else if quote.Gross == 0 {
				v.Reason = quote.Reason
			} else {
				v.State, v.Gross, v.Fee, v.Net = "created", quote.Gross, quote.Fee, quote.Net
				v.Reason = "pending_review"
				afterGross += v.Gross
				afterItems++
				runGross += v.Gross
			}
		}
		items = append(items, v)
	}
	return items, afterGross, afterItems, nil
}
