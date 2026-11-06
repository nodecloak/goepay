package paymentdefend

import (
	"math"
	"math/big"
	"testing"
)

func TestRoundHalfUp2MatchesPHPRound(t *testing.T) {
	for _, c := range []struct {
		num, den int64
		want     string
	}{
		{1, 3, "3333/100"}, // 33.333.. -> 33.33
		{2, 3, "6667/100"}, // 66.666.. -> 66.67
		{1, 8, "1250/100"}, // 12.5 -> 12.5
		{1, 800, "13/100"}, // 0.125 -> 0.13 (half up)
		{0, 5, "0"},
	} {
		got := roundHalfUp2(big.NewRat(c.num*100, c.den))
		want, _ := new(big.Rat).SetString(c.want)
		if got.Cmp(want) != 0 {
			t.Errorf("%d/%d: got %s want %s", c.num, c.den, got.FloatString(4), want.FloatString(4))
		}
	}
}

func TestPHPIntval(t *testing.T) {
	if phpIntval("") != 0 || phpIntval("007") != 7 || phpIntval("99999999999999999999") != math.MaxInt64 {
		t.Fatal("phpIntval diverged from PHP intval for validated digit strings")
	}
}
