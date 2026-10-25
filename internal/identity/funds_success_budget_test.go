package identity

import (
	"context"
	"errors"
	"testing"
)

func TestFundsTwentyValidConfirmationsDoNotExhaustFailureBudget(t *testing.T) {
	_, f, a := fundsFixture(t)
	ctx := context.Background()
	if e := f.SetPassword(ctx, a, fundsLogin, "", fundsPassword, ""); e != nil {
		t.Fatal(e)
	}
	for i := range 20 {
		c, e := f.Prepare(ctx, a, fundsAction(t, "successful-flow"))
		if e != nil {
			t.Fatal(i, e)
		}
		if _, e = f.Authorize(ctx, a, c.ChallengeID, fundsPassword); e != nil {
			t.Fatal("legitimate operation rate limited", i, e)
		}
	}
	c, e := f.Prepare(ctx, a, fundsAction(t, "failure-preserved"))
	if e != nil {
		t.Fatal(e)
	}
	for range 5 {
		if _, e = f.Authorize(ctx, a, c.ChallengeID, "wrong"); !errors.Is(e, ErrFundsAuthentication) {
			t.Fatal(e)
		}
	}
	if _, e = f.Authorize(ctx, a, c.ChallengeID, fundsPassword); !errors.Is(e, ErrFundsRateLimited) {
		t.Fatal("failure budget lost", e)
	}
}
