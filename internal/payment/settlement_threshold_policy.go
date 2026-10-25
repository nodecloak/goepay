package payment

import "time"

const settlementThresholdMode = "balance_threshold"

// SettlementThresholdPolicy models the readable Pro cron/Transfer branch.
// It intentionally has no retained balance, fee minimum, or partial payout.
// This pure quote does not activate a scheduler or create financial intent.
type SettlementThresholdPolicy struct {
	ThresholdCents  int64 `json:"threshold_cents,string"`
	FeeRateBPS      int64 `json:"fee_rate_bps,string"`
	FeeMaximumCents int64 `json:"fee_maximum_cents,string"`
}

type SettlementThresholdQuote struct {
	SettlementScheduledQuote
	Reason string `json:"reason"`
}

func (p SettlementThresholdPolicy) validate() error {
	if p.ThresholdCents <= 0 || p.ThresholdCents > 100000000000 || validateScheduledSettlementPolicy(SettlementPolicy{FeeRateBPS: p.FeeRateBPS, FeeMaximumCents: p.FeeMaximumCents}) != nil {
		return ErrInvalid
	}
	return nil
}

func quoteThresholdSettlement(p SettlementThresholdPolicy, ledgerAvailable, eligible, maxGross int64, at time.Time) (SettlementThresholdQuote, error) {
	q := SettlementThresholdQuote{SettlementScheduledQuote: SettlementScheduledQuote{LedgerAvailable: ledgerAvailable, Eligible: eligible}}
	feePolicy := SettlementPolicy{FeeRateBPS: p.FeeRateBPS, FeeMaximumCents: p.FeeMaximumCents}
	if p.validate() != nil || ledgerAvailable < 0 || eligible < 0 || eligible > ledgerAvailable || maxGross <= 0 {
		return q, ErrInvalid
	}
	var e error
	q.BusinessDate, e = settlementBusinessDate(at, 0)
	if e != nil {
		return q, e
	}
	if ledgerAvailable <= p.ThresholdCents {
		q.Reason = "below_threshold"
		return q, nil
	}
	// The original branch pays the whole balance. Clipping to eligible funds
	// would silently change that product; spending immature funds is forbidden.
	if eligible != ledgerAvailable {
		q.Reason = "funds_not_matured"
		return q, nil
	}
	if ledgerAvailable > maxGross {
		q.Reason = "budget_exhausted"
		return q, nil
	}
	terms, e := feePolicy.Quote(ledgerAvailable)
	if e != nil {
		return q, e
	}
	q.Gross, q.Fee, q.Net = terms.GrossAmount, terms.FeeAmount, terms.NetAmount
	return q, nil
}
