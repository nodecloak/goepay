package payment

import (
	"errors"
	"math"
	"testing"
)

func TestReferralRewardRoundsAwardHalfUpWithoutOverflow(t *testing.T) {
	for _, tc := range []struct{ fee, rate, reward int64 }{
		{1, 5000, 1}, {3, 5000, 2}, {100, 0, 0}, {100, 10000, 100},
		{math.MaxInt64, 0, 0}, {math.MaxInt64, 10000, math.MaxInt64},
		{math.MaxInt64, 5000, 4611686018427387904},
	} {
		got, err := calculateReferralReward(tc.fee, tc.rate)
		if err != nil || got != tc.reward {
			t.Fatalf("fee=%d rate=%d: reward=%d want=%d err=%v", tc.fee, tc.rate, got, tc.reward, err)
		}
	}
	for _, tc := range [][2]int64{{-1, 0}, {1, -1}, {1, 10001}} {
		if _, err := calculateReferralReward(tc[0], tc[1]); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid fee/rate accepted: %v %v", tc, err)
		}
	}
}
