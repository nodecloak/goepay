package payment

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"strings"
)

type MerchantCredentialActor struct {
	User         identity.User
	SessionToken string
	Delegated    bool
}
type MerchantCredentialState struct {
	MerchantID    int64  `json:"merchant_id,string"`
	Revision      int64  `json:"revision,string"`
	Algorithm     string `json:"algorithm"`
	PublicKey     string `json:"public_key"`
	MD5Configured bool   `json:"md5_configured"`
	Fingerprint   string `json:"fingerprint"`
}

func (s *Store) adminMerchantCredentialStateTx(ctx context.Context, tx pgx.Tx, mid int64, a MerchantCredentialActor, write bool) (MerchantCredentialState, error) {
	var out MerchantCredentialState
	if mid <= 0 || tx == nil || s.aead == nil {
		return out, ErrInvalid
	}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	var cipher []byte
	var enabled bool
	e := tx.QueryRow(ctx, "SELECT id,config_revision,signature_algorithm,COALESCE(rsa_public_key,''),api_key,enabled FROM merchants WHERE id=$1"+lock, mid).Scan(&out.MerchantID, &out.Revision, &out.Algorithm, &out.PublicKey, &cipher, &enabled)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if out.Revision <= 0 || (out.Algorithm != "MD5" && out.Algorithm != "RSA") {
		return out, ErrState
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	digest, e := s.referralDigest("admin-merchant-credentials/v1", []any{mid, out.Revision, out.Algorithm, out.PublicKey, cipher, enabled})
	if e != nil {
		return out, e
	}
	out.Fingerprint = hex.EncodeToString(digest)
	out.MD5Configured = len(cipher) >= s.aead.NonceSize()+s.aead.Overhead()
	return out, nil
}

func (s *Store) AdminMerchantCredentialState(ctx context.Context, mid int64, a MerchantCredentialActor) (MerchantCredentialState, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return MerchantCredentialState{}, e
	}
	defer tx.Rollback(ctx)
	out, e := s.adminMerchantCredentialStateTx(ctx, tx, mid, a, false)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func checkMerchantCredentialFingerprint(actual, expected string) error {
	if len(expected) != 64 || subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Store) CheckAdminMerchantCredential(ctx context.Context, mid int64, expected string, a MerchantCredentialActor) error {
	state, e := s.AdminMerchantCredentialState(ctx, mid, a)
	if e != nil {
		return e
	}
	return checkMerchantCredentialFingerprint(state.Fingerprint, expected)
}

// Only an encrypted MD5 key or an RSA public key reaches persistence. RSA
// private material deliberately cannot be passed to this method.
func (s *Store) ReplaceAdminMerchantCredential(ctx context.Context, mid int64, expected, kind, value string, a MerchantCredentialActor) (MerchantCredentialState, error) {
	var empty MerchantCredentialState
	switch kind {
	case "reset-md5":
		raw, e := hex.DecodeString(value)
		if e != nil || len(raw) != 16 || len(value) != 32 || strings.ToLower(value) != value {
			return empty, ErrInvalid
		}
	case "generate-rsa":
		if !validMerchantRSAPublicKey(value) {
			return empty, ErrInvalid
		}
	default:
		return empty, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	state, e := s.adminMerchantCredentialStateTx(ctx, tx, mid, a, true)
	if e != nil {
		return empty, e
	}
	if e = checkMerchantCredentialFingerprint(state.Fingerprint, expected); e != nil {
		return empty, e
	}
	if kind == "reset-md5" {
		nonce := make([]byte, s.aead.NonceSize())
		if _, e = rand.Read(nonce); e != nil {
			return empty, e
		}
		cipher := s.aead.Seal(nonce, nonce, []byte(value), []byte(fmt.Sprint(mid)))
		_, e = tx.Exec(ctx, "UPDATE merchants SET api_key=$2,config_revision=config_revision+1 WHERE id=$1", mid, cipher)
	} else {
		if value == state.PublicKey {
			return empty, ErrInvalid
		}
		_, e = tx.Exec(ctx, "UPDATE merchants SET rsa_public_key=$2,config_revision=config_revision+1 WHERE id=$1", mid, value)
	}
	if e != nil {
		return empty, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'merchants',$3,$4,jsonb_build_object('credential_revision',$5::text))`, a.User.ID, a.User.Username, mid, kind, fmt.Sprint(state.Revision+1)); e != nil {
		return empty, e
	}
	out, e := s.adminMerchantCredentialStateTx(ctx, tx, mid, a, true)
	if e != nil {
		return empty, e
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, e
	}
	return out, nil
}
