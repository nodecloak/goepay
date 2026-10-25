package payment

import (
	"errors"
	"math"
	"testing"
)

func TestReferralClawbackOriginalFeeBudget(t *testing.T) {
	for _, tc := range []struct{ fee, reward, used, refund, contribution int64 }{
		{10, 5, 0, 0, 0}, {10, 5, 0, 3, 0}, {10, 5, 0, 8, 3}, {10, 5, 0, 10, 5},
		{10, 0, 5, 5, 0}, {10, 5, 3, 5, 3}, {math.MaxInt64, math.MaxInt64, 0, math.MaxInt64, math.MaxInt64},
	} {
		k, err := referralClawbackContribution(tc.fee, tc.reward, tc.used, tc.refund)
		if err != nil || k != tc.contribution {
			t.Fatalf("budget %+v got %d %v", tc, k, err)
		}
		// During an unknown outcome, the original remaining budget must be
		// present in fees, the private escrow and this refund's fee hold.
		free := tc.fee - tc.reward - tc.used + k - tc.refund
		if free < 0 || free+(tc.reward-k)+tc.refund != tc.fee-tc.used {
			t.Fatalf("unbalanced %+v", tc)
		}
	}
	for _, tc := range [][4]int64{{-1, 0, 0, 0}, {10, -1, 0, 0}, {10, 11, 0, 0}, {10, 5, 6, 0}, {10, 5, -1, 0}, {10, 5, 0, -1}, {10, 5, 0, 11}, {10, 0, 5, 6}} {
		if _, err := referralClawbackContribution(tc[0], tc[1], tc[2], tc[3]); !errors.Is(err, ErrState) {
			t.Fatalf("accepted invalid original budget %v: %v", tc, err)
		}
	}
}
