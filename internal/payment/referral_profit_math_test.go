package payment

import (
	"math"
	"math/big"
	"math/rand"
	"testing"
)

func TestReferralProfit71ExactDifferenceRoundingAndOriginalFeeBudget(t *testing.T) {
	for _, tc := range []struct {
		amount, fee, units, rate, whole, fraction, profit, reward int64
	}{
		{100, 1, 500000, 10000, 0, 50000000, 1, 1}, // Cost half-cent: do not round it first.
		{100, 1, 500001, 10000, 0, 50000100, 0, 0},
		{100, 1, 499999, 10000, 0, 49999900, 1, 1},
		{100, 10, 1000000, 5000, 1, 0, 9, 5},
		{100, 10, 20000000, 10000, 20, 0, 0, 0}, // Proven negative profit is not unknown cost.
		{100, 10, 0, 10000, 0, 0, 10, 10},
		{100, 0, 1000000, 10000, 1, 0, 0, 0},
		{0, 0, 0, 10000, 0, 0, 0, 0},
		{math.MaxInt64, math.MaxInt64, 100000000, 10000, math.MaxInt64, 0, 0, 0},
	} {
		got, err := calculateReferralProfitCost(tc.amount, tc.fee, tc.units, tc.rate)
		if err != nil || got.CostWhole != tc.whole || got.CostRemainder != tc.fraction || got.CostDenominator != 100000000 || got.Profit != tc.profit || got.Reward != tc.reward {
			t.Fatalf("%+v got=%+v err=%v", tc, got, err)
		}
	}
	for _, bad := range [][4]int64{{-1, 0, 0, 0}, {100, -1, 0, 0}, {100, 101, 0, 0}, {100, 1, -1, 0}, {100, 1, 100000001, 0}, {100, 1, 0, -1}, {100, 1, 0, 10001}} {
		if _, err := calculateReferralProfitCost(bad[0], bad[1], bad[2], bad[3]); err == nil {
			t.Fatal("invalid cost/amount accepted", bad)
		}
	}
}

func TestReferralProfit71MathMatchesIndependentRationalReference(t *testing.T) {
	rng := rand.New(rand.NewSource(710024))
	halfUp := func(v *big.Rat) int64 {
		if v.Sign() <= 0 {
			return 0
		}
		n := new(big.Int).Lsh(new(big.Int).Set(v.Num()), 1)
		n.Add(n, v.Denom())
		d := new(big.Int).Lsh(new(big.Int).Set(v.Denom()), 1)
		return n.Quo(n, d).Int64()
	}
	for i := 0; i < 2000; i++ {
		amount := rng.Int63()
		fee := rng.Int63n(amount + 1)
		units, rate := rng.Int63n(100000001), rng.Int63n(10001)
		cost := new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(amount), big.NewInt(units)), big.NewInt(100000000))
		profit := halfUp(new(big.Rat).Sub(new(big.Rat).SetInt64(fee), cost))
		reward := halfUp(new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(profit), big.NewInt(rate)), big.NewInt(10000)))
		got, err := calculateReferralProfitCost(amount, fee, units, rate)
		if err != nil || got.Profit != profit || got.Reward != reward || got.Reward > fee || got.Reward > got.Profit || got.Profit > fee {
			t.Fatalf("case%d %d/%d/%d/%d got=%+v ref=%d/%d err=%v", i, amount, fee, units, rate, got, profit, reward, err)
		}
	}
}
