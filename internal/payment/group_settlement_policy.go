package payment

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"goepay/internal/ledger"
	"goepay/internal/merchantpolicy"
)

// The original nonempty group settle_rate overrides the global rate, including
// explicit zero. Keep all existing caps, quotas and the sole quote algorithm.
type merchantSettlementSelection struct {
	Policy     SettlementPolicy
	ValidUntil time.Time
}

func readMerchantSettlementSelection(ctx context.Context, q referralReader, mid int64, lock bool) (merchantSettlementSelection, error) {
	var out merchantSettlementSelection
	p, e := readSettlementPolicy(ctx, q, lock)
	out.Policy = p
	if e != nil {
		return out, e
	}
	g, e := merchantpolicy.ReadEffectiveGroup(ctx, q, mid, lock)
	if e != nil {
		if errors.Is(e, merchantpolicy.ErrGroupPolicy) {
			return out, ErrState
		}
		return out, e
	}
	out.ValidUntil = g.ValidUntil
	var fields map[string]json.RawMessage
	if json.Unmarshal(g.Body, &fields) != nil {
		return out, ErrState
	}
	rate, e := thresholdGroupScalar(fields["settle_rate"])
	if e != nil {
		return out, ErrInvalid
	}
	if rate == "" {
		return out, nil
	}
	bps, e := ledger.Parse(rate)
	if e != nil || bps < 0 || bps > 10000 {
		return out, ErrInvalid
	}
	out.Policy.FeeRateBPS = bps
	return out, nil
}

func readMerchantSettlementPolicy(ctx context.Context, q referralReader, mid int64, lock bool) (SettlementPolicy, error) {
	selected, err := readMerchantSettlementSelection(ctx, q, mid, lock)
	return selected.Policy, err
}

// Funds confirmation reads exactly the same current price as a merchant quote.
// This cannot reserve funds or activate a provider/worker.
func (s *Store) FundsSettlementPolicySnapshot(ctx context.Context, q referralReader, mid int64) (SettlementPolicy, error) {
	return readMerchantSettlementPolicy(ctx, q, mid, false)
}
