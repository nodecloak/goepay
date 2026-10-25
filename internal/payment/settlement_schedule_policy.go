package payment

import (
	"math"
	"time"
)

type SettlementScheduleSpec struct {
	Frequency     string    `json:"frequency"`
	Interval      int64     `json:"interval,string"`
	AnchorAt      time.Time `json:"anchor_at"`
	ExecuteHour   int       `json:"execute_hour"`
	ExecuteMinute int       `json:"execute_minute"`
	ExecuteSecond int       `json:"execute_second"`
}

type SettlementScheduledQuote struct {
	LedgerAvailable int64  `json:"ledger_available,string"`
	Eligible        int64  `json:"eligible,string"`
	Retained        int64  `json:"retained,string"`
	Gross           int64  `json:"gross,string"`
	Fee             int64  `json:"fee,string"`
	Net             int64  `json:"net,string"`
	BusinessDate    string `json:"business_date"`
	PolicyHash      []byte `json:"-"`
}

var settlementScheduleZone = time.FixedZone("UTC+08:00", 8*3600)

func validSettlementScheduleTime(at time.Time) bool {
	if at.IsZero() {
		return false
	}
	year := at.In(settlementScheduleZone).Year()
	return year >= 1 && year <= 9999
}

func validateSettlementSchedule(spec SettlementScheduleSpec) error {
	if !validSettlementScheduleTime(spec.AnchorAt) || spec.AnchorAt.Nanosecond() != 0 {
		return ErrInvalid
	}
	if spec.Frequency != "day" && (spec.ExecuteHour != 0 || spec.ExecuteMinute != 0 || spec.ExecuteSecond != 0) {
		return ErrInvalid
	}
	switch spec.Frequency {
	case "second":
		if spec.Interval < 10 || spec.Interval > 3600 {
			return ErrInvalid
		}
	case "minute":
		if spec.Interval < 1 || spec.Interval > 1440 {
			return ErrInvalid
		}
	case "hour":
		if spec.Interval < 1 || spec.Interval > 168 {
			return ErrInvalid
		}
	case "day":
		if spec.Interval < 1 || spec.Interval > 366 || spec.ExecuteHour < 0 || spec.ExecuteHour > 23 || spec.ExecuteMinute < 0 || spec.ExecuteMinute > 59 || spec.ExecuteSecond < 0 || spec.ExecuteSecond > 59 {
			return ErrInvalid
		}
	case "once":
		if spec.Interval != 0 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// Technical cadence support is wider than the financial capability currently
// authorized. Minute threshold and unattended payout remain separate stages.
func validateSettlementPlanSchedule(spec SettlementScheduleSpec, mode string) error {
	if err := validateSettlementSchedule(spec); err != nil {
		return err
	}
	if mode != settlementThresholdMode && (mode != "generate_only" || spec.Frequency == "second" || spec.Frequency == "minute") {
		return ErrInvalid
	}
	return nil
}

func nextSettlementSlot(spec SettlementScheduleSpec, lastSlot int64) (slot int64, at time.Time, err error) {
	if err = validateSettlementSchedule(spec); err != nil {
		return 0, time.Time{}, err
	}
	if lastSlot < -1 {
		return 0, time.Time{}, ErrInvalid
	}
	anchor := spec.AnchorAt.In(settlementScheduleZone)
	if spec.Frequency == "once" {
		if lastSlot >= 0 {
			return 0, time.Time{}, ErrState
		}
		return 0, anchor, nil
	}
	if lastSlot == math.MaxInt64 {
		return 0, time.Time{}, ErrInvalid
	}
	slot = lastSlot + 1
	unit := int64(1)
	switch spec.Frequency {
	case "minute":
		unit = 60
	case "hour":
		unit = 3600
	case "day":
		unit = 86400
		anchor = time.Date(anchor.Year(), anchor.Month(), anchor.Day(), spec.ExecuteHour, spec.ExecuteMinute, spec.ExecuteSecond, 0, settlementScheduleZone)
	}
	step := spec.Interval * unit // bounded intervals make this multiplication safe.
	base := anchor.Unix()
	maximum := time.Date(9999, 12, 31, 23, 59, 59, 0, settlementScheduleZone).Unix()
	// Bound by the supported timestamp range before multiplying. A duration
	// would overflow for valid multi-century schedules, well before year9999.
	if slot > (maximum-base)/step {
		return 0, time.Time{}, ErrInvalid
	}
	return slot, time.Unix(base+slot*step, 0).In(settlementScheduleZone), nil
}

// This labels a business window only. It does not change the fixed-midnight
// D1 availability calculation used by the existing outgoing-funds consumers.
func settlementBusinessDate(at time.Time, cutHour int) (string, error) {
	if !validSettlementScheduleTime(at) || cutHour < 0 || cutHour > 23 {
		return "", ErrInvalid
	}
	local := at.In(settlementScheduleZone).Add(-time.Duration(cutHour) * time.Hour)
	if local.Year() < 1 || local.Year() > 9999 {
		return "", ErrInvalid
	}
	return local.Format("2006-01-02"), nil
}

func retainedByBPS(balance, bps int64) (int64, error) {
	if balance < 0 || bps < 0 || bps > 10000 {
		return 0, ErrInvalid
	}
	keepRate := int64(10000) - bps
	payable := balance/10000*keepRate + (balance%10000*keepRate+5000)/10000
	return balance - payable, nil
}

func validateScheduledSettlementPolicy(p SettlementPolicy) error {
	// Keep configuration bounds identical to the existing policy. Actual
	// fee arithmetic always remains in SettlementPolicy.Quote below.
	for _, value := range []int64{p.MinimumAmount, p.MaximumAmount, p.FeeMinimumCents, p.FeeMaximumCents, p.RetainedCents} {
		if value < 0 || value > 100000000000 {
			return ErrInvalid
		}
	}
	if p.FeeRateBPS < 0 || p.FeeRateBPS > 10000 || p.DailyLimit < 0 || p.DailyLimit > 1000000 || p.MaximumAmount > 0 && p.MinimumAmount > p.MaximumAmount || p.FeeMaximumCents > 0 && p.FeeMinimumCents > p.FeeMaximumCents {
		return ErrInvalid
	}
	return nil
}

func quoteScheduledSettlement(policy SettlementPolicy, ledgerAvailable, eligible, retained, maxGross int64, at time.Time) (SettlementScheduledQuote, error) {
	q := SettlementScheduledQuote{LedgerAvailable: ledgerAvailable, Eligible: eligible, Retained: retained}
	if err := validateScheduledSettlementPolicy(policy); err != nil {
		return q, err
	}
	if ledgerAvailable < 0 || eligible < 0 || eligible > ledgerAvailable || retained < policy.RetainedCents || maxGross < 0 {
		return q, ErrInvalid
	}
	var err error
	q.BusinessDate, err = settlementBusinessDate(at, 0)
	if err != nil {
		return q, err
	}
	if retained >= eligible {
		return q, nil
	}
	q.Gross = eligible - retained
	if policy.MaximumAmount > 0 && q.Gross > policy.MaximumAmount {
		q.Gross = policy.MaximumAmount
	}
	if maxGross > 0 && q.Gross > maxGross {
		q.Gross = maxGross
	}
	terms, err := policy.Quote(q.Gross)
	if err != nil {
		return q, err
	}
	q.Fee, q.Net = terms.FeeAmount, terms.NetAmount
	return q, nil
}
