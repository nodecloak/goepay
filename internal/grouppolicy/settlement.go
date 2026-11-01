package grouppolicy

import (
	"errors"
	"goepay/internal/ledger"
)

var ErrInvalidSettlementMinimum = errors.New("invalid group settlement minimum")
var ErrInvalidSettlementCycle = errors.New("invalid group settlement cycle")

// SettlementMinimum distinguishes inheritance from an explicit zero minimum.
// Cents uses the same bounds and exact units as the settlement quote policy.
type SettlementMinimum struct {
	Cents    int64
	Override bool
}

func ParseSettlementMinimum(value string) (SettlementMinimum, error) {
	if value == "" || value == "_" {
		return SettlementMinimum{}, nil
	}
	cents, err := ledger.Parse(value)
	if err != nil || cents < 0 || cents > 100000000000 {
		return SettlementMinimum{}, ErrInvalidSettlementMinimum
	}
	return SettlementMinimum{Cents: cents, Override: true}, nil
}

// SettlementCycle uses normalized global codes: 0 is D+0 and 1 is D+1.
// An explicit zero is different from the original group's empty inherit value.
type SettlementCycle struct {
	Mode     int
	Override bool
}

func ParseSettlementCycle(value string) (SettlementCycle, error) {
	switch value {
	case "", "_":
		return SettlementCycle{}, nil
	case "0":
		return SettlementCycle{Mode: 0, Override: true}, nil
	case "1":
		return SettlementCycle{Mode: 1, Override: true}, nil
	default:
		return SettlementCycle{}, ErrInvalidSettlementCycle
	}
}
