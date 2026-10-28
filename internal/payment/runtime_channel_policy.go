package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/ledger"
)

// RuntimeChannel is constructible only from the loaded provider registry.
// Its private identity contains no merchant-controlled JSON fields.
type RuntimeChannel struct {
	child                 *ChildExecutionBinding
	id                    int64
	kind                  string
	methods               []string
	product               string
	identity, accountHash []byte
	parent                *parentExecutionBinding
}

func (g ProviderFlow) RuntimeChannel(id int64) (RuntimeChannel, error) {
	if bound, ok := g.boundParents[id]; ok {
		bound.parent = g.parentBindings[id]
		return bound, nil
	}
	if binding := g.parentBindings[id]; binding != nil {
		out := binding.fee
		out.parent = binding
		return out, nil
	}
	var out RuntimeChannel
	p := g.Providers[id]
	if g.Store == nil || id <= 0 || p == nil || !adapter.CatalogPayType(p.ID()) {
		return out, ErrInvalid
	}
	methods := append([]string(nil), p.Capabilities().Methods...)
	if len(methods) == 0 || len(methods) > 32 {
		return out, ErrInvalid
	}
	sort.Strings(methods)
	for i, m := range methods {
		if !providerMethodPattern.MatchString(m) || (i > 0 && m == methods[i-1]) {
			return out, ErrInvalid
		}
	}
	scope, e := adapter.TransferAccountScope(p)
	if e != nil {
		return out, e
	}
	product, e := adapter.PaymentProductScope(p)
	if e != nil {
		return out, e
	}
	identity, e := g.Store.referralDigest("runtime-channel168/identity", []any{id, p.ID(), methods, scope, product})
	if e != nil {
		return out, e
	}
	account, e := g.Store.providerAccountHash(scope)
	if e != nil {
		return out, e
	}
	return RuntimeChannel{id: id, kind: p.ID(), methods: methods, product: product, identity: identity, accountHash: account}, nil
}

type RuntimeChannelPolicy struct {
	CoreRisk    *CoreRiskStatus `json:"core_risk,omitempty"`
	ChannelID   int64           `json:"channel_id,string"`
	PayType     string          `json:"pay_type"`
	Methods     []string        `json:"methods"`
	Configured  bool            `json:"configured"`
	Rate        *string         `json:"rate"`
	FeeRateBPS  *int64          `json:"fee_rate_bps"`
	Mode        int             `json:"mode"`
	Enabled     bool            `json:"enabled"`
	Revision    int64           `json:"revision,string"`
	Fingerprint string          `json:"fingerprint"`
	CanUpdate   bool            `json:"can_update"`
}
type runtimeChannelPolicy struct {
	channel  int64
	kind     string
	identity []byte
	retained int64
	mode     int
	enabled  bool
	revision int64
	proof    []byte
}

func (s *Store) runtimePolicyProof(p runtimeChannelPolicy) ([]byte, error) {
	return s.referralDigest("runtime-channel168/policy", []any{p.channel, p.kind, p.identity, p.retained, p.mode, p.enabled, p.revision})
}
func (s *Store) runtimePolicyTx(ctx context.Context, tx pgx.Tx, b RuntimeChannel, write bool) (runtimeChannelPolicy, bool, error) {
	var p runtimeChannelPolicy
	var committed int64
	if b.id <= 0 || len(b.identity) != 32 {
		return p, false, ErrInvalid
	}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	e := tx.QueryRow(ctx, `SELECT channel_id,pay_type,identity_hash,retained_bps,mode,enabled,revision,proof_hash,(SELECT count(*) FROM backoffice_audit WHERE resource='runtime-channel-policies' AND operation='runtime-channel-policy-update' AND resource_id=$1) FROM runtime_channel_policies WHERE channel_id=$1`+lock, b.id).Scan(&p.channel, &p.kind, &p.identity, &p.retained, &p.mode, &p.enabled, &p.revision, &p.proof, &committed)
	if errors.Is(e, pgx.ErrNoRows) {
		var originated bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-channel-policies' AND operation='runtime-channel-policy-update' AND resource_id=$1)`, b.id).Scan(&originated); e != nil {
			return p, false, e
		}
		if originated {
			return p, false, ErrConflict
		}
		return runtimeChannelPolicy{channel: b.id, kind: b.kind, identity: b.identity, retained: 10000, enabled: true}, false, nil
	}
	if e != nil {
		return p, false, e
	}
	if !s.runtimePolicyValid(b, p, committed) {
		return p, false, ErrConflict
	}
	return p, true, nil
}

// runtimePolicyValid authenticates the same mutable policy in individual and
// batched readers. Append-only update count anchors complete old-row restores.
func (s *Store) runtimePolicyValid(b RuntimeChannel, p runtimeChannelPolicy, updates int64) bool {
	proof, e := s.runtimePolicyProof(p)
	return e == nil && p.channel == b.id && p.kind == b.kind && p.mode == 0 && p.revision > 0 && p.retained >= 0 && p.retained <= 10000 && updates == p.revision && hmac.Equal(p.identity, b.identity) && hmac.Equal(proof, p.proof)
}
func (s *Store) runtimePolicyDTO(b RuntimeChannel, p runtimeChannelPolicy, configured bool) (RuntimeChannelPolicy, error) {
	out := RuntimeChannelPolicy{ChannelID: b.id, PayType: b.kind, Methods: append([]string(nil), b.methods...), Configured: configured, Mode: 0, Enabled: p.enabled, Revision: p.revision, CanUpdate: true}
	if configured {
		rate := ledger.Format(p.retained)
		fee := 10000 - p.retained
		out.Rate = &rate
		out.FeeRateBPS = &fee
	}
	digest, e := s.referralDigest("runtime-channel168/state", []any{b.id, b.kind, b.methods, b.identity, configured, p.retained, p.enabled, p.mode, p.revision, p.proof})
	if e != nil {
		return out, e
	}
	out.Fingerprint = hex.EncodeToString(digest)
	return out, nil
}
func (s *Store) RuntimeChannelPolicies(ctx context.Context, bindings []RuntimeChannel, a MerchantCredentialActor) ([]RuntimeChannelPolicy, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return nil, e
	}
	items := []RuntimeChannelPolicy{}
	for _, b := range bindings {
		p, ok, e := s.runtimePolicyTx(ctx, tx, b, false)
		if e != nil {
			return nil, e
		}
		out, e := s.runtimePolicyDTO(b, p, ok)
		if e != nil {
			return nil, e
		}
		risk, e := s.coreRiskSubjectStatusTx(ctx, tx, "channel", b.id)
		if e != nil {
			return nil, e
		}
		out.CoreRisk = &risk
		items = append(items, out)
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return nil, e
	}
	return items, tx.Commit(ctx)
}
func ParseChannelRetainedRate(raw string) (int64, error) {
	if raw == "" {
		return 10000, nil
	}
	if len(raw) > 6 {
		return 0, ErrInvalid
	}
	parts := strings.Split(raw, ".")
	if len(parts) > 2 || len(parts[0]) == 0 || (len(parts[0]) > 1 && parts[0][0] == '0') {
		return 0, ErrInvalid
	}
	for _, part := range parts {
		if len(part) == 0 {
			return 0, ErrInvalid
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0, ErrInvalid
			}
		}
	}
	if len(parts) == 2 && len(parts[1]) > 2 {
		return 0, ErrInvalid
	}
	n, e := ledger.Parse(raw)
	if e != nil || n > 10000 {
		return 0, ErrInvalid
	}
	return n, nil
}
func (s *Store) UpdateRuntimeChannelPolicy(ctx context.Context, b RuntimeChannel, a MerchantCredentialActor, rate string, mode int, enabled bool, expected string) (RuntimeChannelPolicy, error) {
	var empty RuntimeChannelPolicy
	retained, e := ParseChannelRetainedRate(rate)
	if e != nil || mode != 0 {
		return empty, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", fmt.Sprintf("goepay/route/channel/%d", b.id)); e != nil {
		return empty, e
	}
	p, configured, e := s.runtimePolicyTx(ctx, tx, b, true)
	if e != nil {
		return empty, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return empty, e
	}
	before, e := s.runtimePolicyDTO(b, p, configured)
	if e != nil {
		return empty, e
	}
	if len(expected) != 64 || !hmac.Equal([]byte(expected), []byte(before.Fingerprint)) {
		return empty, ErrConflict
	}
	p.retained, p.mode, p.enabled, p.revision = retained, 0, enabled, p.revision+1
	p.proof, e = s.runtimePolicyProof(p)
	if e != nil {
		return empty, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO runtime_channel_policies(channel_id,pay_type,identity_hash,retained_bps,mode,enabled,revision,proof_hash) VALUES($1,$2,$3,$4,0,$5,$6,$7) ON CONFLICT(channel_id) DO UPDATE SET pay_type=EXCLUDED.pay_type,identity_hash=EXCLUDED.identity_hash,retained_bps=EXCLUDED.retained_bps,mode=0,enabled=EXCLUDED.enabled,revision=EXCLUDED.revision,proof_hash=EXCLUDED.proof_hash`, b.id, b.kind, b.identity, p.retained, p.enabled, p.revision, p.proof); e != nil {
		return empty, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'runtime-channel-policies',$3,'runtime-channel-policy-update',jsonb_build_object('retained_bps',$4::text,'revision',$5::text,'enabled',$6::boolean,'mode',0))`, a.User.ID, a.User.Username, b.id, strconv.FormatInt(p.retained, 10), strconv.FormatInt(p.revision, 10), p.enabled); e != nil {
		return empty, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return empty, e
	}
	out, e := s.runtimePolicyDTO(b, p, true)
	if e != nil {
		return empty, e
	}
	risk, e := s.coreRiskSubjectStatusTx(ctx, tx, "channel", b.id)
	if e != nil {
		return empty, e
	}
	out.CoreRisk = &risk
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return empty, e
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, e
	}
	return out, nil
}

// A missing policy retains the original runtime eligibility. Registered typed
// policies are authority for new route assignments, not for historical repricing.
func (s *Store) RuntimeChannelAllowedTx(ctx context.Context, tx pgx.Tx, b RuntimeChannel) (bool, bool, error) {
	p, configured, e := s.runtimePolicyTx(ctx, tx, b, false)
	return p.enabled, configured, e
}
