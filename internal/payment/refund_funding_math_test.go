package payment

import (
	"math"
	"testing"
)

func TestRefundFundingMathOriginalBudgetsAndHeldSources(t *testing.T) {
	for _, c := range []struct {
		name, payer               string
		g, n, f, ug, um, uf, q, h int64
		m, fee, held, a           int64
	}{
		{"platform", "platform", 100, 90, 10, 0, 0, 0, 98, 90, 90, 8, 90, 0},
		{"merchant", "merchant", 100, 90, 10, 0, 0, 0, 100, 90, 100, 0, 90, 10},
		{"prior success", "platform", 100, 90, 10, 20, 20, 0, 80, 70, 70, 10, 70, 0},
		{"max", "platform", math.MaxInt64, math.MaxInt64, 0, 0, 0, 0, math.MaxInt64, math.MaxInt64, math.MaxInt64, 0, math.MaxInt64, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, f, h, a, e := allocateRefundFunding(c.g, c.n, c.f, c.ug, c.um, c.uf, c.q, c.h, c.payer)
			if e != nil || m != c.m || f != c.fee || h != c.held || a != c.a {
				t.Fatal(m, f, h, a, e)
			}
		})
	}
	if _, _, _, _, e := allocateRefundFunding(100, 90, 10, 20, 20, 0, 81, 70, "platform"); e == nil {
		t.Fatal("over gross accepted")
	}
	if _, _, _, _, e := allocateRefundFunding(100, 90, 10, 0, 0, 0, 10, 91, "platform"); e == nil {
		t.Fatal("over held net accepted")
	}
}
