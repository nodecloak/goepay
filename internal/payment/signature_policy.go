package payment

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"

	"goepay/internal/compat"
	"goepay/internal/identity"
)

type MerchantSignaturePolicy struct {
	MerchantID    int64  `json:"merchant_id,string"`
	Keytype       *int   `json:"keytype"`
	Algorithm     string `json:"algorithm"`
	RSAConfigured bool   `json:"rsa_configured"`
	Fingerprint   string `json:"fingerprint"`
	CanUpdate     bool   `json:"can_update"`
}

type signaturePolicyActor struct {
	merchantID int64
	role, name string
	actorID    int64
	check      func(context.Context, pgx.Tx) error
	admin      bool
}

func merchantSignatureActor(a MerchantRSAActor) signaturePolicyActor {
	return signaturePolicyActor{merchantID: a.User.ID, role: "merchant", name: a.User.Username, actorID: a.User.ID, check: func(ctx context.Context, tx pgx.Tx) error {
		return identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF)
	}}
}

func adminSignatureActor(mid int64, a MerchantCredentialActor) signaturePolicyActor {
	return signaturePolicyActor{merchantID: mid, role: "admin", name: a.User.Username, actorID: a.User.ID, admin: true, check: func(ctx context.Context, tx pgx.Tx) error {
		return identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated)
	}}
}

func usableMerchantRSAPublicKey(public string) bool {
	if len(public) > 8192 {
		return false
	}
	_, e := compat.ParsePublicKey(public)
	return e == nil
}

func (s *Store) signaturePolicyStateTx(ctx context.Context, tx pgx.Tx, a signaturePolicyActor, write bool) (MerchantSignaturePolicy, error) {
	var state MerchantSignaturePolicy
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	var revision int64
	var public string
	var cipher []byte
	var enabled bool
	e := tx.QueryRow(ctx, `SELECT id,config_revision,keytype,signature_algorithm,COALESCE(rsa_public_key,''),api_key,enabled FROM merchants WHERE id=$1`+lock, a.merchantID).Scan(&state.MerchantID, &revision, &state.Keytype, &state.Algorithm, &public, &cipher, &enabled)
	if errors.Is(e, pgx.ErrNoRows) {
		if a.admin {
			return state, ErrNotFound
		}
		return state, identity.ErrOriginalMerchantSession
	}
	if e != nil {
		return state, e
	}
	if !enabled && !a.admin {
		return state, identity.ErrOriginalMerchantSession
	}
	if e = a.check(ctx, tx); e != nil {
		return state, e
	}
	if revision <= 0 || (state.Algorithm != "MD5" && state.Algorithm != "RSA") || (state.Keytype != nil && *state.Keytype != 0 && *state.Keytype != 1) {
		return state, ErrState
	}
	state.RSAConfigured = usableMerchantRSAPublicKey(public)
	digest, e := s.referralDigest("merchant-signature-policy/v1", []any{state.MerchantID, revision, state.Keytype, state.Algorithm, public, cipher, enabled})
	if e != nil {
		return state, e
	}
	state.Fingerprint = hex.EncodeToString(digest)
	state.CanUpdate = true
	return state, nil
}

func (s *Store) MerchantSignaturePolicy(ctx context.Context, a MerchantRSAActor) (MerchantSignaturePolicy, error) {
	return s.signaturePolicyState(ctx, merchantSignatureActor(a))
}

func (s *Store) AdminMerchantSignaturePolicy(ctx context.Context, mid int64, a MerchantCredentialActor) (MerchantSignaturePolicy, error) {
	return s.signaturePolicyState(ctx, adminSignatureActor(mid, a))
}

func (s *Store) signaturePolicyState(ctx context.Context, a signaturePolicyActor) (MerchantSignaturePolicy, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return MerchantSignaturePolicy{}, e
	}
	defer tx.Rollback(ctx)
	state, e := s.signaturePolicyStateTx(ctx, tx, a, false)
	if e != nil {
		return MerchantSignaturePolicy{}, e
	}
	return state, tx.Commit(ctx)
}

func (s *Store) UpdateMerchantSignaturePolicy(ctx context.Context, a MerchantRSAActor, keytype int, expected string) (MerchantSignaturePolicy, error) {
	return s.updateSignaturePolicy(ctx, merchantSignatureActor(a), keytype, expected)
}

func (s *Store) UpdateAdminMerchantSignaturePolicy(ctx context.Context, mid int64, a MerchantCredentialActor, keytype int, expected string) (MerchantSignaturePolicy, error) {
	return s.updateSignaturePolicy(ctx, adminSignatureActor(mid, a), keytype, expected)
}

func (s *Store) updateSignaturePolicy(ctx context.Context, a signaturePolicyActor, keytype int, expected string) (MerchantSignaturePolicy, error) {
	var empty MerchantSignaturePolicy
	if keytype != 0 && keytype != 1 {
		return empty, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	state, e := s.signaturePolicyStateTx(ctx, tx, a, true)
	if e != nil {
		return empty, e
	}
	if len(expected) != 64 || subtle.ConstantTimeCompare([]byte(expected), []byte(state.Fingerprint)) != 1 {
		return empty, ErrConflict
	}
	if keytype == 1 && !state.RSAConfigured {
		return empty, ErrInvalid
	}
	if _, e = tx.Exec(ctx, `UPDATE merchants SET keytype=$2,config_revision=config_revision+1 WHERE id=$1`, state.MerchantID, keytype); e != nil {
		return empty, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'merchants',$4,'signature-policy-update',jsonb_build_object('keytype',$5::int))`, a.role, a.actorID, a.name, state.MerchantID, keytype); e != nil {
		return empty, e
	}
	// Re-read after the final possibly waiting write, then verify the durable
	// original session against the database clock again before commit.
	state, e = s.signaturePolicyStateTx(ctx, tx, a, true)
	if e != nil {
		return empty, e
	}
	if e = a.check(ctx, tx); e != nil {
		return empty, e
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, e
	}
	return state, nil
}
