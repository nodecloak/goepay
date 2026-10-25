package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

type MembershipReferralPolicyInput struct {
	Enabled *bool  `json:"enabled"`
	RateBPS *int64 `json:"rate_bps"`
}

type MembershipReferralPolicyView struct {
	ID       int64  `json:"id,string"`
	Scope    string `json:"scope"`
	ScopeID  int64  `json:"scope_id,string"`
	Revision int64  `json:"revision,string"`
	MembershipReferralPolicyInput
	CreatedAt time.Time `json:"created_at"`
}

type membershipReferralPolicy struct {
	MembershipReferralPolicyView
	Actor     TransferBatchActor
	ProofHash []byte
}

func (s *Store) membershipReferralDigest(purpose string, fact any) ([]byte, error) {
	if len(s.indexKey) != 32 {
		return nil, ErrInvalid
	}
	raw, e := json.Marshal(fact)
	if e != nil || len(raw) > 1<<20 {
		return nil, ErrInvalid
	}
	m := hmac.New(sha256.New, s.indexKey)
	m.Write([]byte("goepay/membership-referral/" + purpose + "/v1\x00"))
	m.Write(raw)
	return m.Sum(nil), nil
}

func membershipReferralPolicyValid(scope string, p MembershipReferralPolicyInput) bool {
	return (scope == "global" || scope == "group") && (p.RateBPS == nil || *p.RateBPS >= 0 && *p.RateBPS <= 10000) && (scope != "global" || p.Enabled != nil && p.RateBPS != nil)
}

func (s *Store) membershipReferralPolicyProof(p membershipReferralPolicy) ([]byte, error) {
	return s.membershipReferralDigest("policy", []any{p.ID, p.Scope, p.ScopeID, p.Revision, p.Enabled, p.RateBPS, p.Actor.Role, p.Actor.ID, p.Actor.Name, p.CreatedAt.UTC().Format(time.RFC3339Nano)})
}

func (s *Store) membershipReferralPolicyByID(ctx context.Context, q referralReader, id int64) (membershipReferralPolicy, error) {
	var p membershipReferralPolicy
	var audited bool
	e := q.QueryRow(ctx, `SELECT p.id,p.scope_kind,p.scope_id,p.revision,p.enabled,p.rate_bps,p.actor_role,p.actor_id,p.actor_name,p.proof_hash,p.created_at,EXISTS(SELECT 1 FROM membership_referral_policy_audits a WHERE a.policy_id=p.id AND (a.actor_role,a.actor_id,a.actor_name)=(p.actor_role,p.actor_id,p.actor_name)) FROM membership_referral_policies p WHERE p.id=$1`, id).Scan(&p.ID, &p.Scope, &p.ScopeID, &p.Revision, &p.Enabled, &p.RateBPS, &p.Actor.Role, &p.Actor.ID, &p.Actor.Name, &p.ProofHash, &p.CreatedAt, &audited)
	if errors.Is(e, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	if e != nil {
		return p, e
	}
	proof, e := s.membershipReferralPolicyProof(p)
	if e != nil || !audited || !hmac.Equal(proof, p.ProofHash) || !validReferralScope(p.Scope, p.ScopeID) || !membershipReferralPolicyValid(p.Scope, p.MembershipReferralPolicyInput) || p.Actor.Role != "admin" || p.Actor.ID <= 0 || p.Actor.Name == "" {
		return p, ErrConflict
	}
	return p, nil
}

func (s *Store) readMembershipReferralPolicy(ctx context.Context, q referralReader, scope string, id int64) (membershipReferralPolicy, error) {
	var pid int64
	e := q.QueryRow(ctx, "SELECT policy_id FROM membership_referral_policy_heads WHERE scope_kind=$1 AND scope_id=$2", scope, id).Scan(&pid)
	if errors.Is(e, pgx.ErrNoRows) {
		return membershipReferralPolicy{}, ErrNotFound
	}
	if e != nil {
		return membershipReferralPolicy{}, e
	}
	p, e := s.membershipReferralPolicyByID(ctx, q, pid)
	if e == nil && (p.Scope != scope || p.ScopeID != id) {
		e = ErrConflict
	}
	if e == nil {
		var latest bool
		e = q.QueryRow(ctx, "SELECT NOT EXISTS(SELECT 1 FROM membership_referral_policies WHERE scope_kind=$1 AND scope_id=$2 AND revision>$3)", scope, id, p.Revision).Scan(&latest)
		if e == nil && !latest {
			e = ErrConflict
		}
	}
	return p, e
}

func (s *Store) FundsMembershipReferralPolicySnapshot(ctx context.Context, q referralReader, scope string, id int64) (MembershipReferralPolicyView, error) {
	if q == nil || !validReferralScope(scope, id) {
		return MembershipReferralPolicyView{}, ErrInvalid
	}
	p, e := s.readMembershipReferralPolicy(ctx, q, scope, id)
	if errors.Is(e, ErrNotFound) {
		return MembershipReferralPolicyView{Scope: scope, ScopeID: id}, nil
	}
	return p.MembershipReferralPolicyView, e
}

func (s *Store) MembershipReferralPolicy(ctx context.Context, scope string, id int64, actor TransferBatchActor) (MembershipReferralPolicyView, error) {
	if actor.Role != "admin" || actor.Class != "super" || !actor.valid(0) || !validReferralScope(scope, id) {
		return MembershipReferralPolicyView{}, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return MembershipReferralPolicyView{}, e
	}
	defer tx.Rollback(ctx)
	return s.FundsMembershipReferralPolicySnapshot(ctx, tx, scope, id)
}

func (s *Store) SetMembershipReferralPolicy(ctx context.Context, scope string, id, expected int64, input MembershipReferralPolicyInput, actor TransferBatchActor) (MembershipReferralPolicyView, error) {
	var out MembershipReferralPolicyView
	if actor.Role != "admin" || actor.Class != "super" || !actor.valid(0) || !validReferralScope(scope, id) || expected < 0 || expected == math.MaxInt64 || !membershipReferralPolicyValid(scope, input) {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", fmt.Sprintf("goepay/membership-referral-policy/%s/%d", scope, id)); e != nil {
		return out, e
	}
	if scope == "group" {
		var exists bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backoffice_records WHERE resource='groups' AND id=$1)", id).Scan(&exists); e != nil {
			return out, e
		}
		if !exists {
			return out, ErrNotFound
		}
	}
	old, e := s.readMembershipReferralPolicy(ctx, tx, scope, id)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return out, e
	}
	if old.Revision != expected {
		return out, ErrConflict
	}
	if e = requireFundsAction(ctx, tx, "membership-referral-policy", fmt.Sprintf("%s:%d:%d", scope, id, expected)); e != nil {
		return out, e
	}
	p := membershipReferralPolicy{MembershipReferralPolicyView: MembershipReferralPolicyView{Scope: scope, ScopeID: id, Revision: expected + 1, MembershipReferralPolicyInput: input}, Actor: actor}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('membership_referral_policies','id')),pg_catalog.clock_timestamp()").Scan(&p.ID, &p.CreatedAt); e != nil {
		return out, e
	}
	p.ProofHash, e = s.membershipReferralPolicyProof(p)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO membership_referral_policies(id,scope_kind,scope_id,revision,enabled,rate_bps,actor_role,actor_id,actor_name,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, p.ID, scope, id, p.Revision, p.Enabled, p.RateBPS, actor.Role, actor.ID, actor.Name, p.ProofHash, p.CreatedAt); e != nil {
		return out, e
	}
	if expected == 0 {
		_, e = tx.Exec(ctx, "INSERT INTO membership_referral_policy_heads(scope_kind,scope_id,policy_id) VALUES($1,$2,$3)", scope, id, p.ID)
	} else {
		var tag interface{ RowsAffected() int64 }
		tag, e = tx.Exec(ctx, "UPDATE membership_referral_policy_heads SET policy_id=$3 WHERE scope_kind=$1 AND scope_id=$2 AND policy_id=$4", scope, id, p.ID, old.ID)
		if e == nil && tag.RowsAffected() != 1 {
			e = ErrConflict
		}
	}
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO membership_referral_policy_audits(policy_id,actor_role,actor_id,actor_name) VALUES($1,$2,$3,$4)", p.ID, actor.Role, actor.ID, actor.Name); e != nil {
		return out, e
	}
	return p.MembershipReferralPolicyView, tx.Commit(ctx)
}
