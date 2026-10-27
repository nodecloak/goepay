package payment

import (
	"context"
	"crypto/hmac"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
)

var channelLimitZone = time.FixedZone("UTC+8", 8*3600)

type parentLimitFacts struct {
	Channel                    int64
	Identity                   []byte
	PolicyRevision             int64
	PolicyProof                []byte
	Limits                     parentGlobalLimits
	Gross                      int64
	ObservedAt, ValidUntil     time.Time
	SuccessCount, RecentStarts int64
	SuccessMoney               string
}

// The public type has no exported construction fields or merchant JSON input.
type ParentGlobalLimitAdmission struct{ facts parentLimitFacts }

func (v *ParentGlobalLimitAdmission) MinAmount() int64 {
	if v == nil {
		return 0
	}
	return v.facts.Limits.PayMin
}
func (v *ParentGlobalLimitAdmission) MaxAmount() int64 {
	if v == nil {
		return 0
	}
	return v.facts.Limits.PayMax
}
func (v *ParentGlobalLimitAdmission) ValidUntil() time.Time {
	if v == nil {
		return time.Time{}
	}
	return v.facts.ValidUntil
}

type RuntimeChannelLimitUsage struct {
	Day              string `json:"day"`
	SuccessfulAmount string `json:"successful_amount"`
	SuccessfulCount  string `json:"successful_count"`
	RecentStarts     string `json:"recent_starts"`
	DayPaused        bool   `json:"day_paused"`
	RateLimited      bool   `json:"rate_limited"`
	AsOf             string `json:"as_of"`
}

func globalDay(now time.Time) (string, time.Time, time.Time) {
	local := now.In(channelLimitZone)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, channelLimitZone)
	return start.Format("2006-01-02"), start, start.AddDate(0, 0, 1)
}
func formatBigMoney(cents *big.Int) string {
	whole, rem := new(big.Int), new(big.Int)
	whole.QuoRem(cents, big.NewInt(100), rem)
	return fmt.Sprintf("%s.%02d", whole.String(), rem.Int64())
}
func (v parentGlobalLimits) hourAllows(now time.Time) (bool, time.Time) {
	if v.TimeStart == nil || v.TimeStop == nil || *v.TimeStart == *v.TimeStop {
		return true, time.Time{}
	}
	local := now.In(channelLimitZone)
	hour := local.Hour()
	start, stop := *v.TimeStart, *v.TimeStop
	if start < stop {
		if hour < start || hour > stop {
			return false, time.Time{}
		}
	} else if hour < start && hour > stop {
		return false, time.Time{}
	}
	until := time.Date(local.Year(), local.Month(), local.Day(), stop+1, 0, 0, 0, channelLimitZone)
	if start > stop && hour >= start {
		until = until.AddDate(0, 0, 1)
	}
	return true, until
}
func (s *Store) channelLimitSchema(ctx context.Context, q fundsReader) (bool, error) {
	var tables int
	var migrations bool
	if e := q.QueryRow(ctx, `SELECT num_nonnulls(to_regclass(current_schema()||'.runtime_channel_limits'),to_regclass(current_schema()||'.runtime_channel_success_events'),to_regclass(current_schema()||'.runtime_channel_success_days')),to_regclass(current_schema()||'.schema_migrations') IS NOT NULL`).Scan(&tables, &migrations); e != nil {
		return false, e
	}
	if tables == 3 {
		return true, nil
	}
	var migrated bool
	if migrations {
		if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000099_runtime_channel_limits.up.sql')`).Scan(&migrated); e != nil {
			return false, e
		}
	}
	if tables != 0 || migrated {
		return false, ErrConflict
	}
	// The append-only policy/success origin survives loss of every 99 table
	// and its migration registration. Absence of a new order's quote is not
	// evidence that a formerly configured database predates this feature.
	var auditTable bool
	if e := q.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.backoffice_audit') IS NOT NULL`).Scan(&auditTable); e != nil {
		return false, e
	}
	if auditTable {
		var originated bool
		if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE (resource='runtime-channel-limits' AND operation='runtime-channel-limits-update') OR (resource='runtime-channel-success' AND operation='runtime-channel-success'))`).Scan(&originated); e != nil {
			return false, e
		}
		if originated {
			return false, ErrConflict
		}
	}
	return false, nil
}
func (s *Store) globalRecentStarts(ctx context.Context, q referralReader, channel, seconds int64, now time.Time) (int64, error) {
	if seconds <= 0 {
		return 0, nil
	}
	var count int64
	e := q.QueryRow(ctx, `SELECT count(*) FROM (
 SELECT r.merchant_id,r.out_trade_no FROM provider_route_assignments r WHERE r.channel_id=$1 AND r.created_at >= $2::timestamptz-$3::bigint*interval '1 second'
 UNION ALL SELECT o.merchant_id,o.out_trade_no FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE a.channel_id=$1 AND o.created_at >= $2::timestamptz-$3::bigint*interval '1 second' AND NOT EXISTS(SELECT 1 FROM provider_route_assignments r WHERE r.merchant_id=o.merchant_id AND r.out_trade_no=o.out_trade_no)) starts`, channel, now, seconds).Scan(&count)
	return count, e
}
func (s *Store) globalLimitUsage(ctx context.Context, tx pgx.Tx, channel int64, limits parentGlobalLimits, now time.Time) (RuntimeChannelLimitUsage, int64, *big.Int, error) {
	day, start, end := globalDay(now)
	out := RuntimeChannelLimitUsage{Day: day, AsOf: now.UTC().Format(time.RFC3339Nano)}
	count, money, e := s.globalSuccessfulTotal(ctx, tx, channel, day, start, end)
	if e != nil {
		return out, 0, nil, e
	}
	recent, e := s.globalRecentStarts(ctx, tx, channel, limits.RateLimitSeconds, now)
	if e != nil {
		return out, 0, nil, e
	}
	out.SuccessfulAmount = formatBigMoney(money)
	out.SuccessfulCount = fmt.Sprint(count)
	out.RecentStarts = fmt.Sprint(recent)
	out.DayPaused = limits.DayTop > 0 && money.Cmp(big.NewInt(limits.DayTop)) >= 0 || limits.DayMaxOrder > 0 && count >= limits.DayMaxOrder
	out.RateLimited = limits.RateLimitSeconds > 0 && limits.RateLimitCount > 0 && recent >= limits.RateLimitCount
	return out, recent, money, nil
}

// Caller holds the shared parent advisory lock. This only reads eligibility;
// child candidate loops cannot consume another start. The chosen assignment is
// the original single start and is sealed by the routing transaction.
func (s *Store) CheckRuntimeChannelLimitsTx(ctx context.Context, tx pgx.Tx, b RuntimeChannel, gross int64, now time.Time) (bool, *ParentGlobalLimitAdmission, error) {
	if tx == nil || gross <= 0 || now.IsZero() {
		return false, nil, ErrInvalid
	}
	ready, e := s.channelLimitSchema(ctx, tx)
	if e != nil {
		return false, nil, e
	}
	if !ready {
		return true, nil, nil
	}
	p, configured, e := s.globalLimitPolicyTx(ctx, tx, b, false)
	if e != nil {
		return false, nil, e
	}
	if !configured {
		return true, nil, nil
	}
	v := p.Limits
	if v.PayMin > 0 && gross < v.PayMin || v.PayMax > 0 && gross > v.PayMax {
		return false, nil, nil
	}
	allowed, until := v.hourAllows(now)
	if !allowed {
		return false, nil, nil
	}
	usage, recent, money, e := s.globalLimitUsage(ctx, tx, b.id, v, now)
	if e != nil {
		return false, nil, e
	}
	if usage.DayPaused || usage.RateLimited {
		return false, nil, nil
	}
	var count int64
	if _, e = fmt.Sscan(usage.SuccessfulCount, &count); e != nil {
		return false, nil, e
	}
	admission := &ParentGlobalLimitAdmission{facts: parentLimitFacts{Channel: b.id, Identity: b.identity, PolicyRevision: p.Revision, PolicyProof: p.Proof, Limits: v, Gross: gross, ObservedAt: now, ValidUntil: until, SuccessCount: count, SuccessMoney: money.String(), RecentStarts: recent}}
	return true, admission, nil
}
func (s *Store) validateLimitAdmission(b RuntimeChannel, gross int64, a *ParentGlobalLimitAdmission) error {
	if a == nil {
		return nil
	}
	f := a.facts
	p := runtimeGlobalLimitPolicy{Channel: f.Channel, Kind: b.kind, Identity: f.Identity, Limits: f.Limits, Revision: f.PolicyRevision}
	proof, e := s.globalLimitProof(p)
	if e != nil {
		return e
	}
	if f.Channel != b.id || gross != f.Gross || f.ObservedAt.IsZero() || f.PolicyRevision <= 0 || !hmac.Equal(f.Identity, b.identity) || !hmac.Equal(proof, f.PolicyProof) {
		return ErrConflict
	}
	return nil
}
func limitDeadline(ctx context.Context, tx pgx.Tx, until time.Time) error {
	if until.IsZero() {
		return nil
	}
	var now time.Time
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	if !now.Before(until) {
		return ErrState
	}
	return nil
}
func (s *Store) validateGlobalLimitFirstAttempt(ctx context.Context, tx pgx.Tx, channel int64, quote *routeFeeQuote) error {
	ready, e := s.channelLimitSchema(ctx, tx)
	if e != nil {
		return e
	}
	if !ready {
		if quote != nil && quote.facts.GlobalLimits != nil {
			return ErrConflict
		}
		return nil
	}
	if quote != nil {
		return nil
	} // Original committed route facts are not re-admitted.
	var configured, originated bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_channel_limits WHERE channel_id=$1),EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-channel-limits' AND resource_id=$1)`, channel).Scan(&configured, &originated); e != nil {
		return e
	}
	if configured {
		return ErrState
	}
	if originated {
		return ErrConflict
	}
	return nil
}
