package payment

import (
	"errors"
	"math"
	"testing"
)

func TestBuyerCharge104OriginalMerchantAndBuyerRoundingAndMinimumReplacement(t *testing.T) {
	for _, tc := range []struct {
		name             string
		m, rate          int64
		mode             int
		minimum          buyerMinimum104
		charge, net, fee int64
	}{
		{"merchant-six-percent", 10000, 600, 0, buyerMinimum104{}, 10000, 9400, 600},
		{"buyer-six-percent", 10000, 600, 1, buyerMinimum104{}, 10600, 10000, 600},
		{"merchant-half-cent-fee-round", 100, 50, 0, buyerMinimum104{}, 100, 99, 1},
		{"buyer-half-cent-additive-round", 100, 50, 1, buyerMinimum104{}, 101, 100, 1},
		{"merchant-one-cent-half", 1, 5000, 0, buyerMinimum104{}, 1, 0, 1},
		{"buyer-one-cent-half", 1, 5000, 1, buyerMinimum104{}, 2, 1, 1},
		{"merchant-minimum-replaces", 100, 100, 0, buyerMinimum104{Threshold: 5, Cost: 2}, 100, 98, 2},
		{"buyer-minimum-replaces", 100, 100, 1, buyerMinimum104{Threshold: 5, Cost: 2}, 102, 100, 2},
		{"minimum-is-replacement-not-max", 100, 500, 1, buyerMinimum104{Threshold: 10, Cost: 1}, 101, 100, 1},
		{"threshold-equality-does-not-replace", 100, 500, 1, buyerMinimum104{Threshold: 5, Cost: 8}, 105, 100, 5},
		{"merchant-minimum-net-floors-zero", 100, 100, 0, buyerMinimum104{Threshold: 5, Cost: 200}, 100, 0, 100},
		{"buyer-minimum-can-exceed-original", 100, 100, 1, buyerMinimum104{Threshold: 5, Cost: 200}, 300, 100, 200},
		{"only-one-positive-minimum-is-inactive", 100, 100, 1, buyerMinimum104{Threshold: 5}, 101, 100, 1},
		{"maximum-merchant-principal", math.MaxInt64, 10000, 0, buyerMinimum104{}, math.MaxInt64, 0, math.MaxInt64},
		{"maximum-zero-buyer-fee", math.MaxInt64, 0, 1, buyerMinimum104{}, math.MaxInt64, math.MaxInt64, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, e := calculateBuyerCharge104(tc.m, tc.rate, tc.mode, nil, tc.minimum)
			if e != nil || got.Original != tc.m || got.Charge != tc.charge || got.Net != tc.net || got.Fee != tc.fee {
				t.Fatal(got, e)
			}
			if got.Net+got.Fee != got.Charge {
				t.Fatal("unbalanced actual charge", got)
			}
		})
	}
}

func TestBuyerCharge104RejectsInvalidInputAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		m, rate int64
		mode    int
		minimum buyerMinimum104
	}{
		{0, 100, 0, buyerMinimum104{}}, {-1, 100, 0, buyerMinimum104{}}, {100, -1, 0, buyerMinimum104{}}, {100, 10001, 0, buyerMinimum104{}}, {100, 100, 2, buyerMinimum104{}}, {100, 100, 0, buyerMinimum104{Threshold: -1}}, {math.MaxInt64, 1, 1, buyerMinimum104{}}, {math.MaxInt64, 0, 1, buyerMinimum104{Threshold: 1, Cost: 1}},
	} {
		if got, e := calculateBuyerCharge104(tc.m, tc.rate, tc.mode, nil, tc.minimum); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid or overflow price became actionable", tc, got, e)
		}
	}
	zero, total := int64(0), int64(600)
	if got, e := calculateBuyerCharge104(10000, 600, 1, &zero, buyerMinimum104{}); e != nil || got.Charge != 10000 || got.Net != 9400 {
		t.Fatal(got, e)
	}
	if got, e := calculateBuyerCharge104(10000, 600, 1, &total, buyerMinimum104{}); e != nil || got.Charge != 10600 || got.Net != 10000 {
		t.Fatal(got, e)
	}
}
