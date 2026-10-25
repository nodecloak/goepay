package payment

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestSettlementScheduleUTC8BoundariesAndFamilies(t *testing.T) {
	zone := time.FixedZone("test UTC+08:00", 8*3600)
	anchor := time.Date(2026, 10, 23, 23, 59, 40, 0, zone)
	for _, tc := range []struct {
		family         string
		interval, last int64
		want           time.Time
	}{
		{"second", 10, -1, anchor}, {"second", 10, 1, time.Date(2026, 10, 24, 0, 0, 0, 0, zone)},
		{"minute", 2, 0, time.Date(2026, 10, 24, 0, 1, 40, 0, zone)},
		{"hour", 2, 2, time.Date(2026, 10, 24, 5, 59, 40, 0, zone)},
	} {
		t.Run(tc.family+tc.want.Format("150405"), func(t *testing.T) {
			spec := SettlementScheduleSpec{Frequency: tc.family, Interval: tc.interval, AnchorAt: anchor.UTC()}
			slot, at, err := nextSettlementSlot(spec, tc.last)
			_, offset := at.Zone()
			if err != nil || slot != tc.last+1 || !at.Equal(tc.want) || offset != 8*3600 {
				t.Fatalf("slot=%d at=%s want=%s: %v", slot, at, tc.want, err)
			}
		})
	}
	day := SettlementScheduleSpec{Frequency: "day", Interval: 1, AnchorAt: time.Date(2024, 2, 29, 7, 15, 0, 0, zone), ExecuteHour: 0}
	for _, tc := range []struct {
		last int64
		want time.Time
	}{{-1, time.Date(2024, 2, 29, 0, 0, 0, 0, zone)}, {0, time.Date(2024, 3, 1, 0, 0, 0, 0, zone)}, {365, time.Date(2025, 3, 1, 0, 0, 0, 0, zone)}} {
		slot, at, err := nextSettlementSlot(day, tc.last)
		if err != nil || slot != tc.last+1 || !at.Equal(tc.want) {
			t.Fatalf("leap/explicit midnight slot%d %s want%s %v", slot, at, tc.want, err)
		}
	}
	day.Interval = 366
	day.AnchorAt = time.Date(2023, 3, 1, 23, 0, 0, 0, zone)
	day.ExecuteHour = 8
	day.ExecuteMinute = 9
	day.ExecuteSecond = 10
	if _, at, err := nextSettlementSlot(day, 0); err != nil || !at.Equal(time.Date(2024, 3, 1, 8, 9, 10, 0, zone)) {
		t.Fatalf("day anchor interval %s %v", at, err)
	}
	once := SettlementScheduleSpec{Frequency: "once", AnchorAt: anchor}
	if slot, at, err := nextSettlementSlot(once, -1); err != nil || slot != 0 || !at.Equal(anchor) {
		t.Fatalf("once %d %s %v", slot, at, err)
	}
	for _, last := range []int64{0, 1, math.MaxInt64} {
		if _, _, err := nextSettlementSlot(once, last); !errors.Is(err, ErrState) {
			t.Fatalf("once rearmed at %d: %v", last, err)
		}
	}
	for _, tc := range []struct {
		at   time.Time
		cut  int
		want string
	}{
		{time.Date(2026, 10, 23, 15, 59, 59, 999999999, time.UTC), 0, "2026-10-23"},
		{time.Date(2026, 10, 23, 16, 0, 0, 0, time.UTC), 0, "2026-10-24"},
		{time.Date(2026, 10, 24, 7, 59, 59, 0, zone), 8, "2026-10-23"},
		{time.Date(2026, 10, 24, 8, 0, 0, 0, zone), 8, "2026-10-24"},
	} {
		got, err := settlementBusinessDate(tc.at, tc.cut)
		if err != nil || got != tc.want {
			t.Fatalf("business date %s cut%d =%s want%s %v", tc.at, tc.cut, got, tc.want, err)
		}
	}
}

func TestSettlementScheduleUTC8StrictValidationAndOverflow(t *testing.T) {
	zone := time.FixedZone("test UTC+08:00", 8*3600)
	anchor := time.Date(2026, 10, 23, 0, 0, 0, 0, zone)
	for _, tc := range []struct {
		family   string
		min, max int64
	}{{"second", 10, 3600}, {"minute", 1, 1440}, {"hour", 1, 168}, {"day", 1, 366}} {
		for _, interval := range []int64{tc.min, tc.max} {
			if err := validateSettlementSchedule(SettlementScheduleSpec{Frequency: tc.family, Interval: interval, AnchorAt: anchor}); err != nil {
				t.Fatalf("valid boundary %s/%d %v", tc.family, interval, err)
			}
		}
		for _, interval := range []int64{-1, 0, tc.min - 1, tc.max + 1, math.MaxInt64} {
			if err := validateSettlementSchedule(SettlementScheduleSpec{Frequency: tc.family, Interval: interval, AnchorAt: anchor}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted interval %s/%d %v", tc.family, interval, err)
			}
		}
	}
	invalid := []SettlementScheduleSpec{
		{}, {Frequency: "Hour", Interval: 1, AnchorAt: anchor}, {Frequency: "hour ", Interval: 1, AnchorAt: anchor},
		{Frequency: "month", Interval: 1, AnchorAt: anchor}, {Frequency: "hour", Interval: 1},
		{Frequency: "hour", Interval: 1, AnchorAt: anchor.Add(time.Nanosecond)},
		{Frequency: "hour", Interval: 1, AnchorAt: time.Date(10000, 1, 1, 0, 0, 0, 0, zone)},
		{Frequency: "hour", Interval: 1, AnchorAt: anchor, ExecuteHour: 1},
		{Frequency: "minute", Interval: 1, AnchorAt: anchor, ExecuteMinute: 1},
		{Frequency: "second", Interval: 10, AnchorAt: anchor, ExecuteSecond: 1},
		{Frequency: "once", Interval: 1, AnchorAt: anchor}, {Frequency: "once", AnchorAt: anchor, ExecuteSecond: 1},
	}
	for _, field := range []string{"hour", "minute", "second"} {
		for _, n := range []int{-1, 24, 60} {
			spec := SettlementScheduleSpec{Frequency: "day", Interval: 1, AnchorAt: anchor}
			switch field {
			case "hour":
				spec.ExecuteHour = n
			case "minute":
				spec.ExecuteMinute = n
			case "second":
				spec.ExecuteSecond = n
			}
			if field != "hour" && n == 24 {
				continue
			}
			invalid = append(invalid, spec)
		}
	}
	for _, spec := range invalid {
		if err := validateSettlementSchedule(spec); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted malformed typed schedule %+v: %v", spec, err)
		}
	}
	for _, family := range []string{"hour", "day", "once"} {
		spec := SettlementScheduleSpec{Frequency: family, Interval: 1, AnchorAt: anchor}
		if family == "once" {
			spec.Interval = 0
		}
		if err := validateSettlementPlanSchedule(spec, "generate_only"); err != nil {
			t.Fatalf("valid first-stage schedule %+v %v", spec, err)
		}
	}
	for _, spec := range []SettlementScheduleSpec{{Frequency: "second", Interval: 10, AnchorAt: anchor}, {Frequency: "minute", Interval: 1, AnchorAt: anchor}} {
		if err := validateSettlementPlanSchedule(spec, "generate_only"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unsupported fast financial schedule %+v %v", spec, err)
		}
	}
	for _, mode := range []string{"", "auto_payout", "threshold", "GENERATE_ONLY"} {
		if err := validateSettlementPlanSchedule(SettlementScheduleSpec{Frequency: "hour", Interval: 1, AnchorAt: anchor}, mode); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unsupported financial mode %s %v", mode, err)
		}
	}
	periodic := SettlementScheduleSpec{Frequency: "second", Interval: 10, AnchorAt: anchor}
	for _, last := range []int64{-2, math.MinInt64, math.MaxInt64, math.MaxInt64 - 1} {
		if _, _, err := nextSettlementSlot(periodic, last); !errors.Is(err, ErrInvalid) {
			t.Fatalf("slot overflow %d %v", last, err)
		}
	}
	periodic.AnchorAt = time.Date(9999, 12, 31, 23, 59, 59, 0, zone)
	if _, _, err := nextSettlementSlot(periodic, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("year10000 slot %v", err)
	}
	// This supported span is over 9,000 years, far beyond time.Duration's
	// roughly 292-year range. A duration multiplication must not wrap it.
	periodic.AnchorAt = time.Date(1, 1, 1, 0, 0, 0, 0, zone)
	maxTime := time.Date(9999, 12, 31, 23, 59, 59, 0, zone)
	maxSlot := (maxTime.Unix() - periodic.AnchorAt.Unix()) / 10
	if slot, at, err := nextSettlementSlot(periodic, maxSlot-1); err != nil || slot != maxSlot || !at.Equal(time.Unix(periodic.AnchorAt.Unix()+maxSlot*10, 0)) {
		t.Fatalf("large anchored slot %d %s %v", slot, at, err)
	}
	if _, _, err := nextSettlementSlot(periodic, maxSlot); !errors.Is(err, ErrInvalid) {
		t.Fatalf("out-of-range large slot %v", err)
	}
	for _, tc := range []struct {
		at  time.Time
		cut int
	}{{time.Time{}, 0}, {anchor, -1}, {anchor, 24}, {periodic.AnchorAt, 1}, {time.Date(10000, 1, 1, 0, 0, 0, 0, zone), 0}} {
		if _, err := settlementBusinessDate(tc.at, tc.cut); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid business time %s/%d %v", tc.at, tc.cut, err)
		}
	}
}

func TestSettlementScheduleRetainedAndFeeBudgets(t *testing.T) {
	for _, tc := range []struct{ balance, bps, retained int64 }{{1, 5000, 0}, {3, 5000, 1}, {100, 10000, 100}, {100, 0, 0}, {math.MaxInt64, 0, 0}, {math.MaxInt64, 10000, math.MaxInt64}, {math.MaxInt64, 5000, 4611686018427387903}} {
		got, err := retainedByBPS(tc.balance, tc.bps)
		if err != nil || got != tc.retained {
			t.Fatalf("retained %+v =%d %v", tc, got, err)
		}
	}
	for _, tc := range [][2]int64{{-1, 0}, {100, -1}, {100, 10001}, {math.MaxInt64, math.MaxInt64}} {
		if _, err := retainedByBPS(tc[0], tc[1]); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid retention %v %v", tc, err)
		}
	}
	at := time.Date(2026, 10, 23, 16, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		policy                                           SettlementPolicy
		ledger, eligible, retained, cap, gross, fee, net int64
	}{
		{SettlementPolicy{}, 100, 100, 100, 0, 0, 0, 0},
		{SettlementPolicy{}, 0, 0, 100, 0, 0, 0, 0},
		{SettlementPolicy{FeeRateBPS: 1000}, 200, 100, 20, 0, 80, 8, 72},
		{SettlementPolicy{MaximumAmount: 30, FeeRateBPS: 1000}, 200, 100, 20, 0, 30, 3, 27},
		{SettlementPolicy{MaximumAmount: 30, FeeRateBPS: 1000}, 200, 100, 20, 15, 15, 2, 13},
		{SettlementPolicy{RetainedCents: 20}, 200, 100, 20, 0, 80, 0, 80},
		{SettlementPolicy{FeeMinimumCents: 10}, 1, 1, 0, 0, 1, 0, 1},
		{SettlementPolicy{FeeRateBPS: 5000}, 3, 3, 0, 0, 3, 2, 1},
		{SettlementPolicy{}, math.MaxInt64, math.MaxInt64, 0, 0, math.MaxInt64, 0, math.MaxInt64},
	} {
		q, err := quoteScheduledSettlement(tc.policy, tc.ledger, tc.eligible, tc.retained, tc.cap, at)
		if err != nil || q.LedgerAvailable != tc.ledger || q.Eligible != tc.eligible || q.Retained != tc.retained || q.Gross != tc.gross || q.Fee != tc.fee || q.Net != tc.net || q.BusinessDate != "2026-10-24" || len(q.PolicyHash) != 0 {
			t.Fatalf("quote %+v => %+v %v", tc, q, err)
		}
	}
	for _, tc := range []struct {
		policy                          SettlementPolicy
		ledger, eligible, retained, cap int64
	}{
		{SettlementPolicy{}, -1, 0, 0, 0}, {SettlementPolicy{}, 100, 101, 0, 0}, {SettlementPolicy{}, 100, -1, 0, 0}, {SettlementPolicy{}, 100, 100, -1, 0}, {SettlementPolicy{}, 100, 100, 0, -1},
		{SettlementPolicy{RetainedCents: 20}, 100, 100, 19, 0},
		{SettlementPolicy{MinimumAmount: 81}, 200, 100, 20, 0},
		{SettlementPolicy{FeeRateBPS: 10000}, 100, 100, 0, 0},
		{SettlementPolicy{FeeRateBPS: 5000}, 1, 1, 0, 0},
		{SettlementPolicy{FeeRateBPS: 1, FeeMinimumCents: 3}, 2, 2, 0, 0},
		{SettlementPolicy{FeeRateBPS: -1}, 0, 0, 0, 0}, {SettlementPolicy{FeeRateBPS: 10001}, 100, 100, 0, 0},
		{SettlementPolicy{MinimumAmount: -1}, 0, 0, 0, 0}, {SettlementPolicy{MaximumAmount: 100000000001}, 0, 0, 0, 0},
		{SettlementPolicy{MinimumAmount: 2, MaximumAmount: 1}, 0, 0, 0, 0}, {SettlementPolicy{FeeMinimumCents: 2, FeeMaximumCents: 1}, 0, 0, 0, 0},
		{SettlementPolicy{DailyLimit: 1000001}, 0, 0, 0, 0},
	} {
		if q, err := quoteScheduledSettlement(tc.policy, tc.ledger, tc.eligible, tc.retained, tc.cap, at); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid quote %+v => %+v %v", tc, q, err)
		}
	}
	if _, err := quoteScheduledSettlement(SettlementPolicy{}, 100, 100, 0, 0, time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("quote without business time %v", err)
	}
}

func TestSettlementScheduleRetainedUsesExactIntegerOracleAndStableWireTypes(t *testing.T) {
	for _, balance := range []int64{0, 1, 2, 3, 9999, 10000, 10001, math.MaxInt64 - 1, math.MaxInt64} {
		previous := int64(0)
		for _, bps := range []int64{0, 1, 3333, 5000, 9999, 10000} {
			payable := new(big.Int).Mul(big.NewInt(balance), big.NewInt(10000-bps))
			payable.Add(payable, big.NewInt(5000))
			payable.Quo(payable, big.NewInt(10000))
			want := balance - payable.Int64()
			got, err := retainedByBPS(balance, bps)
			if err != nil || got != want || got < previous || got < 0 || got > balance {
				t.Fatalf("exact retained L%d bps%d =%d want%d prior%d %v", balance, bps, got, want, previous, err)
			}
			previous = got
		}
	}
	at := time.Date(2026, 10, 23, 16, 0, 0, 0, time.UTC)
	q, err := quoteScheduledSettlement(SettlementPolicy{FeeRateBPS: 3333}, math.MaxInt64, math.MaxInt64, 0, 0, at)
	wantFee := new(big.Int).Mul(big.NewInt(math.MaxInt64), big.NewInt(3333))
	wantFee.Add(wantFee, big.NewInt(5000))
	wantFee.Quo(wantFee, big.NewInt(10000))
	if err != nil || q.Fee != wantFee.Int64() || q.Net != math.MaxInt64-q.Fee || q.Net <= 0 {
		t.Fatalf("MaxInt64 fee overflow %+v %v", q, err)
	}
	q.PolicyHash = []byte("internal keyed policy proof")
	raw, err := json.Marshal(q)
	if err != nil || !strings.Contains(string(raw), `"gross":"9223372036854775807"`) || strings.Contains(string(raw), "policy_hash") || strings.Contains(string(raw), "PolicyHash") {
		t.Fatalf("unsafe public quote wire %s %v", raw, err)
	}
	spec := SettlementScheduleSpec{Frequency: "hour", Interval: 1, AnchorAt: at}
	raw, err = json.Marshal(spec)
	if err != nil || !strings.Contains(string(raw), `"interval":"1"`) {
		t.Fatalf("schedule interval contract %s %v", raw, err)
	}
	if err = json.Unmarshal([]byte(`{"frequency":"hour","interval":1,"anchor_at":"2026-10-23T16:00:00Z"}`), &spec); err == nil {
		t.Fatal("typed interval accepted a numeric alias")
	}
	if err = json.Unmarshal([]byte(`{"frequency":"hour","interval":"1.5","anchor_at":"2026-10-23T16:00:00Z"}`), &spec); err == nil {
		t.Fatal("typed interval accepted a fractional value")
	}
}
