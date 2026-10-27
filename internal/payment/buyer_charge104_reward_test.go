package payment

import (
	"testing"
)

func TestBuyerCharge104NewGrossRewardUsesActualAndRetainsWholeAwardVoid(t *testing.T) {
	for _, mode := range []string{"immediate", "daily"} {
		t.Run(mode, func(t *testing.T) {
			s, promoter, mid, _ := referralRewardFixture(t)
			yes, rate := true, int64(500)
			policy := referralTestPolicy(&yes, &rate)
			policy.Basis = "gross"
			policy.Mode = mode
			if _, e := s.SetReferralPolicy(t.Context(), "global", 0, 1, policy, referralPolicyAdmin); e != nil {
				t.Fatal(e)
			}
			buyer104PolicyFixture(t, s, "merchant", mid, 1, nil, nil)
			o := referralCreateTestOrder(t, s, mid, 100, "gross-actual104")
			tx, e := s.pool.Begin(t.Context())
			if e != nil {
				t.Fatal(e)
			}
			terms, e := s.readReferralTerms(t.Context(), tx, o)
			tx.Rollback(t.Context())
			if e != nil || terms.RewardAmount != 6 || o.Amount != 100 {
				t.Fatal("new gross reward calculated on requested M", terms.RewardAmount, e)
			}
			if mode == "daily" && (terms.daily == nil || terms.daily.Amount != 110) {
				t.Fatal("daily source did not freeze actual110", terms.daily)
			}
			if _, _, e = s.BeginProviderAttempt(t.Context(), o.TradeNo, 7, "native"); e != nil {
				t.Fatal(e)
			}
			if e = s.markPaid(t.Context(), o.TradeNo, "synthetic-reward104", true, PaidReceipt{RealMoney: 110, ChannelID: 7, ProviderTradeNo: "LOCAL104_" + o.TradeNo}); e != nil {
				t.Fatal(e)
			}
			r, e := s.ReserveRefund(t.Context(), mid, o.OutTradeNo, "one-cent104", 1)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.ResolveRefund(t.Context(), r.ID, "succeeded"); e != nil {
				t.Fatal(e)
			}
			balance, e := s.Balance(t.Context(), promoter)
			if e != nil || balance.Available != 0 {
				t.Fatal("original any-successful-refund whole-award void changed", balance, e)
			}
			if r, e = s.ReserveRefund(t.Context(), mid, o.OutTradeNo, "actual-remainder104", 109); e != nil || r.Amount != 109 || r.FeeAmount != 10 {
				t.Fatal("reward source capped actual refund at M", r, e)
			}
		})
	}
}

func TestBuyerCharge104NewProfitCostUsesActualPayable(t *testing.T) {
	s, _, mid, _ := referralRewardFixture(t)
	yes, rate := true, int64(5000)
	policy := referralTestPolicy(&yes, &rate)
	policy.Basis = "profit"
	if _, e := s.SetReferralPolicy(t.Context(), "global", 0, 1, policy, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	buyer104PolicyFixture(t, s, "merchant", mid, 1, nil, nil)
	o, e := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: mid, OutTradeNo: "profit-actual104", Name: "synthetic", Type: "wxpay", Amount: 100, PaymentCost: profitQuote71(t, "5")})
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(t.Context())
	terms, e := s.readReferralTerms(t.Context(), tx, o)
	if e != nil || terms.profit == nil {
		t.Fatal(terms, e)
	}
	cost := terms.profit
	if cost.Amount != 110 || cost.CostWhole != 5 || cost.CostRemainder != 50000000 || o.Amount != 100 {
		t.Fatal("provider cost was calculated on requested M", cost.Amount, cost.CostWhole, cost.CostRemainder, o.Amount)
	}
}
