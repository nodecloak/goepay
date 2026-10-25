package payment

import (
	"math"
	"math/big"
	"testing"
	"time"
)

func TestSettlementThreshold74UsesStrictThresholdWholeBalanceAndOriginalFeeRules(t *testing.T) {
	at := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name                                      string
		policy                                    SettlementThresholdPolicy
		balance, eligible, limit, gross, fee, net int64
		reason                                    string
	}{
		{"equal threshold", SettlementThresholdPolicy{ThresholdCents: 100}, 100, 100, 1000, 0, 0, 0, "below_threshold"},
		{"strictly above", SettlementThresholdPolicy{ThresholdCents: 100, FeeRateBPS: 300}, 101, 101, 1000, 101, 3, 98, ""},
		{"zero rate", SettlementThresholdPolicy{ThresholdCents: 100, FeeMaximumCents: 50}, 101, 101, 1000, 101, 0, 101, ""},
		{"fee maximum", SettlementThresholdPolicy{ThresholdCents: 100, FeeRateBPS: 1000, FeeMaximumCents: 5}, 101, 101, 1000, 101, 5, 96, ""},
		{"positive half up", SettlementThresholdPolicy{ThresholdCents: 100, FeeRateBPS: 5000}, 101, 101, 1000, 101, 51, 50, ""},
		{"never trim to budget", SettlementThresholdPolicy{ThresholdCents: 100}, 101, 101, 100, 0, 0, 0, "budget_exhausted"},
		{"never spend immature income", SettlementThresholdPolicy{ThresholdCents: 100}, 101, 100, 1000, 0, 0, 0, "funds_not_matured"},
	} {
		t.Run(c.name, func(t *testing.T) {
			q, e := quoteThresholdSettlement(c.policy, c.balance, c.eligible, c.limit, at)
			if e != nil || q.Gross != c.gross || q.Fee != c.fee || q.Net != c.net || q.Reason != c.reason || q.Retained != 0 || q.BusinessDate != "2026-10-24" {
				t.Fatal(q, e)
			}
		})
	}
}

func TestSettlementThreshold74BoundariesAgreeWithIndependentIntegerOracle(t *testing.T) {
	at := time.Now()
	for _, amount := range []int64{2, 3, 5, 101, 9999, 10000, 10001, 99999999999, math.MaxInt64} {
		for _, rate := range []int64{0, 1, 50, 3333, 5000, 9999, 10000} {
			for _, cap := range []int64{0, 1, 999} {
				q, e := quoteThresholdSettlement(SettlementThresholdPolicy{ThresholdCents: 1, FeeRateBPS: rate, FeeMaximumCents: cap}, amount, amount, math.MaxInt64, at)
				n := new(big.Int).Mul(big.NewInt(amount), big.NewInt(rate))
				n.Add(n, big.NewInt(5000))
				n.Div(n, big.NewInt(10000))
				fee := n.Int64()
				if cap > 0 && fee > cap {
					fee = cap
				}
				if fee >= amount {
					if e == nil {
						t.Fatal("zero-net automatic transfer", amount, rate, cap, q)
					}
					continue
				}
				if e != nil || q.Gross != amount || q.Fee != fee || q.Net != amount-fee {
					t.Fatal("original rate/max arithmetic mismatch", amount, rate, cap, q, e)
				}
			}
		}
	}
	for _, p := range []SettlementThresholdPolicy{{}, {ThresholdCents: -1}, {ThresholdCents: 100000000001}, {ThresholdCents: 1, FeeRateBPS: -1}, {ThresholdCents: 1, FeeRateBPS: 10001}, {ThresholdCents: 1, FeeMaximumCents: -1}, {ThresholdCents: 1, FeeMaximumCents: 100000000001}} {
		if _, e := quoteThresholdSettlement(p, 100, 100, 100, at); e == nil {
			t.Fatal("invalid original threshold contract", p)
		}
	}
}
