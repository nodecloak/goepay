package payment

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type ReferralPolicyInput struct {
	Enabled      *bool  `json:"enabled"`
	RateBPS      *int64 `json:"rate_bps"`
	Mode         string `json:"mode"`
	Basis        string `json:"basis"`
	RefundPolicy string `json:"refund_policy"`
}

type ReferralPolicyView struct {
	ID       int64  `json:"id,string"`
	Scope    string `json:"scope"`
	ScopeID  int64  `json:"scope_id,string"`
	Revision int64  `json:"revision,string"`
	ReferralPolicyInput
	CreatedAt time.Time `json:"created_at"`
}

type referralPolicyRecord struct {
	ReferralPolicyView
	Actor     TransferBatchActor
	ProofHash []byte
}

func validReferralScope(scope string, id int64) bool {
	return scope == "global" && id == 0 || scope == "group" && id > 0
}

func validateReferralPolicy(scope string, p ReferralPolicyInput) error {
	if p.Mode != "immediate" && p.Mode != "daily" || p.Basis != "fee" && p.Basis != "gross" && p.Basis != "profit" || p.RefundPolicy != referralRefundPolicy || p.RateBPS != nil && (*p.RateBPS < 0 || *p.RateBPS > 10000) || scope == "global" && (p.Enabled == nil || p.RateBPS == nil) {
		return ErrInvalid
	}
	return nil
}

func (s *Store) referralPolicyProof(r referralPolicyRecord) ([]byte, error) {
	return s.referralDigest("policy-version", []any{r.ID, r.Scope, r.ScopeID, r.Revision, r.Enabled, r.RateBPS, r.Mode, r.Basis, r.RefundPolicy, r.Actor.Role, r.Actor.ID, r.Actor.Name})
}

func (s *Store) readReferralPolicyTx(ctx context.Context, tx referralReader, scope string, id int64) (referralPolicyRecord, error) {
	var r referralPolicyRecord
	var audited bool
	err := tx.QueryRow(ctx, `SELECT p.id,p.scope_kind,p.scope_id,p.revision,p.enabled,p.rate_bps,p.mode,p.basis,p.refund_policy,p.actor_role,p.actor_id,p.actor_name,p.proof_hash,p.created_at,
 EXISTS(SELECT 1 FROM referral_policy_audits a WHERE a.policy_id=p.id AND (a.actor_role,a.actor_id,a.actor_name)=(p.actor_role,p.actor_id,p.actor_name))
 FROM referral_policy_heads h JOIN referral_policies p ON p.id=h.policy_id AND p.scope_kind=h.scope_kind AND p.scope_id=h.scope_id WHERE h.scope_kind=$1 AND h.scope_id=$2`, scope, id).Scan(
		&r.ID, &r.Scope, &r.ScopeID, &r.Revision, &r.Enabled, &r.RateBPS, &r.Mode, &r.Basis, &r.RefundPolicy, &r.Actor.Role, &r.Actor.ID, &r.Actor.Name, &r.ProofHash, &r.CreatedAt, &audited)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	proof, err := s.referralPolicyProof(r)
	if err != nil || !hmac.Equal(proof, r.ProofHash) || !audited || validateReferralPolicy(r.Scope, r.ReferralPolicyInput) != nil {
		return r, ErrConflict
	}
	return r, nil
}

// FundsReferralPolicySnapshot authenticates a policy in the caller's existing
// read/authorization snapshot. The HTTP caller authorizes the real admin.
func (s *Store) FundsReferralPolicySnapshot(ctx context.Context, q referralReader, scope string, id int64) (ReferralPolicyView, error) {
	if !validReferralScope(scope, id) || q == nil {
		return ReferralPolicyView{}, ErrInvalid
	}
	r, err := s.readReferralPolicyTx(ctx, q, scope, id)
	if errors.Is(err, ErrNotFound) {
		return ReferralPolicyView{Scope: scope, ScopeID: id, ReferralPolicyInput: ReferralPolicyInput{Mode: "immediate", Basis: "fee", RefundPolicy: referralRefundPolicy}}, nil
	}
	return r.ReferralPolicyView, err
}

func (s *Store) ReferralPolicy(ctx context.Context, scope string, id int64, actor TransferBatchActor) (ReferralPolicyView, error) {
	if !validReferralScope(scope, id) || actor.Role != "admin" || actor.Class != "super" || !actor.valid(0) {
		return ReferralPolicyView{}, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ReferralPolicyView{}, err
	}
	defer tx.Rollback(ctx)
	r, err := s.readReferralPolicyTx(ctx, tx, scope, id)
	if errors.Is(err, ErrNotFound) {
		return ReferralPolicyView{Scope: scope, ScopeID: id, ReferralPolicyInput: ReferralPolicyInput{Mode: "immediate", Basis: "fee", RefundPolicy: referralRefundPolicy}}, nil
	}
	return r.ReferralPolicyView, err
}

func (s *Store) SetReferralPolicy(ctx context.Context, scope string, id, expected int64, input ReferralPolicyInput, actor TransferBatchActor) (ReferralPolicyView, error) {
	var out ReferralPolicyView
	if !validReferralScope(scope, id) || expected < 0 || actor.Role != "admin" || actor.Class != "super" || !actor.valid(0) || validateReferralPolicy(scope, input) != nil {
		return out, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", fmt.Sprintf("goepay/referral-policy/%s/%d", scope, id)); err != nil {
		return out, err
	}
	if scope == "group" {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backoffice_records WHERE resource='groups' AND id=$1)", id).Scan(&exists); err != nil {
			return out, err
		}
		if !exists {
			return out, ErrNotFound
		}
	}
	old, err := s.readReferralPolicyTx(ctx, tx, scope, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return out, err
	}
	if old.Revision != expected {
		return out, ErrConflict
	}
	if err = requireFundsAction(ctx, tx, "referral-policy", fmt.Sprintf("%s:%d:%d", scope, id, expected)); err != nil {
		return out, err
	}
	r := referralPolicyRecord{ReferralPolicyView: ReferralPolicyView{Scope: scope, ScopeID: id, Revision: expected + 1, ReferralPolicyInput: input}, Actor: actor}
	if err = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('referral_policies','id'))").Scan(&r.ID); err != nil {
		return out, err
	}
	hash, err := s.referralPolicyProof(r)
	if err != nil {
		return out, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO referral_policies(id,scope_kind,scope_id,revision,enabled,rate_bps,mode,basis,refund_policy,actor_role,actor_id,actor_name,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING created_at`,
		r.ID, scope, id, r.Revision, input.Enabled, input.RateBPS, input.Mode, input.Basis, input.RefundPolicy, actor.Role, actor.ID, actor.Name, hash).Scan(&r.CreatedAt); err != nil {
		return out, err
	}
	if expected == 0 {
		if _, err = tx.Exec(ctx, `INSERT INTO referral_policy_heads(scope_kind,scope_id,policy_id) VALUES($1,$2,$3)`, scope, id, r.ID); err != nil {
			return out, err
		}
	} else {
		tag, updateErr := tx.Exec(ctx, `UPDATE referral_policy_heads SET policy_id=$3 WHERE scope_kind=$1 AND scope_id=$2 AND policy_id=$4`, scope, id, r.ID, old.ID)
		if updateErr != nil {
			return out, updateErr
		}
		if tag.RowsAffected() != 1 {
			return out, ErrConflict
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO referral_policy_audits(policy_id,actor_role,actor_id,actor_name) VALUES($1,$2,$3,$4)", r.ID, actor.Role, actor.ID, actor.Name); err != nil {
		return out, err
	}
	return r.ReferralPolicyView, tx.Commit(ctx)
}

type effectiveReferralPolicy struct {
	Enabled bool
	RateBPS int64
	Basis   string
	Mode    string
}

func (s *Store) effectiveReferralPolicy(ctx context.Context, tx pgx.Tx, payerGroup, promoterID int64) (effectiveReferralPolicy, [5]int64, bool, error) {
	policy := effectiveReferralPolicy{Basis: "fee", Mode: "immediate"}
	refs := [5]int64{0, 0, 0, payerGroup, 0}
	var enabled, unresolved bool
	var promoterGroup int64
	err := tx.QueryRow(ctx, "SELECT enabled,group_baseline_unresolved,base_group_id FROM merchants WHERE id=$1 FOR SHARE", promoterID).Scan(&enabled, &unresolved, &promoterGroup)
	if err != nil {
		return policy, refs, false, err
	}
	if unresolved {
		return policy, refs, false, ErrState
	}
	var purchased int64
	err = tx.QueryRow(ctx, "SELECT group_id FROM memberships WHERE merchant_id=$1 AND expires_at>statement_timestamp()", promoterID).Scan(&purchased)
	if err == nil {
		promoterGroup = purchased
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return policy, refs, false, err
	}
	refs[4] = promoterGroup
	for i, scope := range []struct {
		kind string
		id   int64
	}{{"global", 0}, {"group", payerGroup}, {"group", promoterGroup}} {
		if scope.kind == "group" && scope.id == 0 {
			continue
		}
		r, err := s.readReferralPolicyTx(ctx, tx, scope.kind, scope.id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return policy, refs, false, err
		}
		refs[i] = r.ID
		policy.Basis = r.Basis
		policy.Mode = r.Mode
		if r.Enabled != nil {
			policy.Enabled = *r.Enabled
		}
		if r.RateBPS != nil {
			policy.RateBPS = *r.RateBPS
		}
	}
	return policy, refs, enabled, nil
}
