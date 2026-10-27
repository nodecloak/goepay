package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"goepay/internal/identity"
	"goepay/internal/ledger"
)

type RuntimeChannelLimitsInput struct {
	DayTop           string `json:"daytop"`
	DayMaxOrder      string `json:"daymaxorder"`
	PayMin           string `json:"paymin"`
	PayMax           string `json:"paymax"`
	TimeStart        string `json:"timestart"`
	TimeStop         string `json:"timestop"`
	RateLimitSeconds string `json:"rate_limit_seconds"`
	RateLimitCount   string `json:"rate_limit_count"`
}
type RuntimeChannelLimitsState struct {
	RuntimeChannelLimitsInput
	ChannelID   int64                    `json:"channel_id,string"`
	PayType     string                   `json:"pay_type"`
	Configured  bool                     `json:"configured"`
	Revision    int64                    `json:"revision,string"`
	Fingerprint string                   `json:"fingerprint"`
	CanUpdate   bool                     `json:"can_update"`
	Timezone    string                   `json:"timezone"`
	Semantics   string                   `json:"semantics"`
	Usage       RuntimeChannelLimitUsage `json:"usage"`
}
type parentGlobalLimits struct {
	DayTop, DayMaxOrder, PayMin, PayMax int64
	TimeStart, TimeStop                 *int
	RateLimitSeconds, RateLimitCount    int64
}
type runtimeGlobalLimitPolicy struct {
	Channel  int64
	Kind     string
	Identity []byte
	Limits   parentGlobalLimits
	Revision int64
	Proof    []byte
}

var limitMoneyPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,2})?$`)
var limitIntegerPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

func parseLimitMoney(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	if !limitMoneyPattern.MatchString(raw) {
		return 0, ErrInvalid
	}
	n, e := ledger.Parse(raw)
	if e != nil || n < 0 {
		return 0, ErrInvalid
	}
	return n, nil
}
func parseLimitCount(raw string, max int64) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	if !limitIntegerPattern.MatchString(raw) {
		return 0, ErrInvalid
	}
	n, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || n < 0 || n > max {
		return 0, ErrInvalid
	}
	return n, nil
}
func parseLimitHour(raw string) (*int, error) {
	if raw == "" {
		return nil, nil
	}
	n, e := parseLimitCount(raw, 23)
	if e != nil {
		return nil, e
	}
	v := int(n)
	return &v, nil
}
func parseRuntimeLimits(in RuntimeChannelLimitsInput) (parentGlobalLimits, error) {
	var v parentGlobalLimits
	var e error
	if v.DayTop, e = parseLimitMoney(in.DayTop); e != nil {
		return v, e
	}
	if v.DayMaxOrder, e = parseLimitCount(in.DayMaxOrder, int64(^uint64(0)>>1)); e != nil {
		return v, e
	}
	if v.PayMin, e = parseLimitMoney(in.PayMin); e != nil {
		return v, e
	}
	if v.PayMax, e = parseLimitMoney(in.PayMax); e != nil {
		return v, e
	}
	if v.PayMax > 0 && v.PayMin > v.PayMax {
		return v, ErrInvalid
	}
	if v.TimeStart, e = parseLimitHour(in.TimeStart); e != nil {
		return v, e
	}
	if v.TimeStop, e = parseLimitHour(in.TimeStop); e != nil {
		return v, e
	}
	if (v.TimeStart == nil) != (v.TimeStop == nil) {
		return v, ErrInvalid
	}
	if v.RateLimitSeconds, e = parseLimitCount(in.RateLimitSeconds, 2147483647); e != nil {
		return v, e
	}
	if v.RateLimitCount, e = parseLimitCount(in.RateLimitCount, int64(^uint64(0)>>1)); e != nil {
		return v, e
	}
	return v, nil
}
func (v parentGlobalLimits) input() RuntimeChannelLimitsInput {
	out := RuntimeChannelLimitsInput{DayTop: ledger.Format(v.DayTop), DayMaxOrder: strconv.FormatInt(v.DayMaxOrder, 10), PayMin: ledger.Format(v.PayMin), PayMax: ledger.Format(v.PayMax), RateLimitSeconds: strconv.FormatInt(v.RateLimitSeconds, 10), RateLimitCount: strconv.FormatInt(v.RateLimitCount, 10)}
	if v.TimeStart != nil {
		out.TimeStart = strconv.Itoa(*v.TimeStart)
		out.TimeStop = strconv.Itoa(*v.TimeStop)
	}
	return out
}
func (s *Store) globalLimitProof(p runtimeGlobalLimitPolicy) ([]byte, error) {
	return s.referralDigest("channel168c/limits-policy", []any{p.Channel, p.Kind, p.Identity, p.Limits, p.Revision})
}
func (s *Store) globalLimitPolicyTx(ctx context.Context, tx pgx.Tx, b RuntimeChannel, write bool) (runtimeGlobalLimitPolicy, bool, error) {
	p := runtimeGlobalLimitPolicy{Channel: b.id, Kind: b.kind, Identity: b.identity}
	if b.id <= 0 || len(b.identity) != 32 {
		return p, false, ErrInvalid
	}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	e := tx.QueryRow(ctx, `SELECT channel_id,pay_type,identity_hash,daytop,daymaxorder,paymin,paymax,timestart,timestop,rate_limit_seconds,rate_limit_count,revision,proof_hash FROM runtime_channel_limits WHERE channel_id=$1`+lock, b.id).Scan(&p.Channel, &p.Kind, &p.Identity, &p.Limits.DayTop, &p.Limits.DayMaxOrder, &p.Limits.PayMin, &p.Limits.PayMax, &p.Limits.TimeStart, &p.Limits.TimeStop, &p.Limits.RateLimitSeconds, &p.Limits.RateLimitCount, &p.Revision, &p.Proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var exists bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-channel-limits' AND operation='runtime-channel-limits-update' AND resource_id=$1)`, b.id).Scan(&exists); e != nil {
			return p, false, e
		}
		if exists {
			return p, false, ErrConflict
		}
		return p, false, nil
	}
	if e != nil {
		return p, false, e
	}
	proof, e := s.globalLimitProof(p)
	if e != nil {
		return p, false, e
	}
	if (p.Limits.TimeStart == nil) != (p.Limits.TimeStop == nil) {
		return p, false, ErrConflict
	}
	parsed, e := parseRuntimeLimits(p.Limits.input())
	if e != nil || p.Channel != b.id || p.Kind != b.kind || p.Revision <= 0 || !hmac.Equal(b.identity, p.Identity) || !hmac.Equal(proof, p.Proof) {
		return p, false, ErrConflict
	}
	// An old authentic policy must not restore revoked limits. Each committed
	// save appends exactly one independent audit under the same parent lock;
	// fee-policy changes and payment success events use other audit resources.
	var committed int64
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='runtime-channel-limits' AND resource_id=$1 AND operation='runtime-channel-limits-update'`, b.id).Scan(&committed); e != nil {
		return p, false, e
	}
	if committed != p.Revision {
		return p, false, ErrConflict
	}
	p.Limits = parsed
	return p, true, nil
}
func (s *Store) globalLimitDTO(ctx context.Context, tx pgx.Tx, b RuntimeChannel, p runtimeGlobalLimitPolicy, configured bool) (RuntimeChannelLimitsState, error) {
	out := RuntimeChannelLimitsState{RuntimeChannelLimitsInput: p.Limits.input(), ChannelID: b.id, PayType: b.kind, Configured: configured, Revision: p.Revision, CanUpdate: true, Timezone: "UTC+08:00", Semantics: "successful-threshold-v1"}
	parent, parentConfigured, e := s.runtimePolicyTx(ctx, tx, b, false)
	if e != nil {
		return out, e
	}
	proof, e := s.referralDigest("channel168c/limits-state", []any{b.id, b.kind, b.identity, configured, p.Limits, p.Revision, p.Proof, parentConfigured, parent.revision, parent.proof})
	if e != nil {
		return out, e
	}
	out.Fingerprint = hex.EncodeToString(proof)
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return out, e
	}
	out.Usage, _, _, e = s.globalLimitUsage(ctx, tx, b.id, p.Limits, now)
	if e != nil {
		return out, e
	}
	return out, nil
}
func (s *Store) RuntimeChannelLimits(ctx context.Context, b RuntimeChannel, a MerchantCredentialActor) (RuntimeChannelLimitsState, error) {
	var out RuntimeChannelLimitsState
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", fmt.Sprintf("goepay/route/channel/%d", b.id)); e != nil {
		return out, e
	}
	p, configured, e := s.globalLimitPolicyTx(ctx, tx, b, false)
	if e != nil {
		return out, e
	}
	out, e = s.globalLimitDTO(ctx, tx, b, p, configured)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) UpdateRuntimeChannelLimits(ctx context.Context, b RuntimeChannel, a MerchantCredentialActor, in RuntimeChannelLimitsInput, expected string) (RuntimeChannelLimitsState, error) {
	var out RuntimeChannelLimitsState
	limits, e := parseRuntimeLimits(in)
	if e != nil {
		return out, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", fmt.Sprintf("goepay/route/channel/%d", b.id)); e != nil {
		return out, e
	}
	p, configured, e := s.globalLimitPolicyTx(ctx, tx, b, true)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	before, e := s.globalLimitDTO(ctx, tx, b, p, configured)
	if e != nil {
		return out, e
	}
	if len(expected) != 64 || !hmac.Equal([]byte(expected), []byte(before.Fingerprint)) {
		return out, ErrConflict
	}
	p.Limits = limits
	p.Revision++
	p.Proof, e = s.globalLimitProof(p)
	if e != nil {
		return out, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO runtime_channel_limits(channel_id,pay_type,identity_hash,daytop,daymaxorder,paymin,paymax,timestart,timestop,rate_limit_seconds,rate_limit_count,revision,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(channel_id) DO UPDATE SET pay_type=EXCLUDED.pay_type,identity_hash=EXCLUDED.identity_hash,daytop=EXCLUDED.daytop,daymaxorder=EXCLUDED.daymaxorder,paymin=EXCLUDED.paymin,paymax=EXCLUDED.paymax,timestart=EXCLUDED.timestart,timestop=EXCLUDED.timestop,rate_limit_seconds=EXCLUDED.rate_limit_seconds,rate_limit_count=EXCLUDED.rate_limit_count,revision=EXCLUDED.revision,proof_hash=EXCLUDED.proof_hash`, b.id, b.kind, b.identity, limits.DayTop, limits.DayMaxOrder, limits.PayMin, limits.PayMax, limits.TimeStart, limits.TimeStop, limits.RateLimitSeconds, limits.RateLimitCount, p.Revision, p.Proof)
	if e != nil {
		return out, e
	}
	changes, _ := json.Marshal(limits.input())
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'runtime-channel-limits',$3,'runtime-channel-limits-update',$4::jsonb)`, a.User.ID, a.User.Username, b.id, string(changes)); e != nil {
		return out, e
	}
	out, e = s.globalLimitDTO(ctx, tx, b, p, true)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
