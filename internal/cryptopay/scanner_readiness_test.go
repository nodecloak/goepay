package cryptopay

import (
	"context"
	"errors"
	"testing"

	"goepay/internal/cryptochain"
)

type readinessChain struct {
	scanChain
	finalized uint64
}

func (c *readinessChain) FinalizedHeight(context.Context) (uint64, error) {
	return c.finalized, nil
}

func TestScannerListenerReadinessRequiresCaughtUpCanonicalCursor(t *testing.T) {
	service := testService(t)
	primary := &readinessChain{scanChain: scanChain{hash: "synthetic-hash"}, finalized: 1}
	witness := &readinessChain{scanChain: scanChain{hash: "synthetic-hash"}, finalized: 1}
	scanner := &Scanner{
		Service:     service,
		Primary:     map[string]cryptochain.Adapter{"tron": primary},
		Witness:     map[string]cryptochain.Adapter{"tron": witness},
		StartHeight: map[string]uint64{"tron": 2},
		MaxBlocks:   1,
	}
	if checked, err := scanner.ScanForReadiness(t.Context(), "tron"); err != nil || !checked.IsZero() {
		t.Fatalf("unstarted listener reported readiness: %s %v", checked, err)
	}
	scanner.StartHeight["tron"] = 1
	primary.finalized = 4
	witness.finalized = 3
	for height := 1; height <= 3; height++ {
		checked, err := scanner.ScanForReadiness(t.Context(), "tron")
		if err != nil || checked.IsZero() != (height < 3) {
			t.Fatalf("scan at %d reported incorrect readiness %s: %v", height, checked, err)
		}
	}
	if checked, err := scanner.ScanForReadiness(t.Context(), "tron"); err != nil || checked.IsZero() {
		t.Fatalf("caught-up listener without new blocks failed readiness: %s %v", checked, err)
	}
	witness.finalized = 2
	if checked, err := scanner.ScanForReadiness(t.Context(), "tron"); err != nil || !checked.IsZero() {
		t.Fatalf("cursor ahead of agreed finality was reported ready: %s %v", checked, err)
	}
	witness.hash = "different-canonical-hash"
	if checked, err := scanner.ScanForReadiness(t.Context(), "tron"); !errors.Is(err, ErrConflict) || !checked.IsZero() {
		t.Fatalf("disagreeing node produced readiness: %s %v", checked, err)
	}
}
