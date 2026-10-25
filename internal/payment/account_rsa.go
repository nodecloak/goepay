package payment

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

var ErrMerchantRSAActor = errors.New("merchant RSA actor unavailable")
var ErrMerchantRSAPassword = errors.New("merchant RSA password rejected")
var ErrMerchantRSAConflict = errors.New("merchant RSA key changed; refresh before retrying")

type MerchantRSAActor struct {
	User        identity.User
	Token, CSRF string
}
type MerchantRSAState struct {
	Algorithm   string `json:"algorithm"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
}

// The fingerprint binds the exact existing key and algorithm, never API secrets.
func MerchantRSAFingerprint(algorithm, public string) string {
	h := sha256.Sum256([]byte("goepay/merchant-rsa/v1\x00" + algorithm + "\x00" + public))
	return hex.EncodeToString(h[:])
}
func validMerchantRSAPublicKey(raw string) bool {
	if len(raw) > 8192 || !strings.HasPrefix(raw, "-----BEGIN PUBLIC KEY-----\n") {
		return false
	}
	b, rest := pem.Decode([]byte(raw))
	if b == nil || b.Type != "PUBLIC KEY" || len(b.Headers) != 0 || strings.TrimSpace(string(rest)) != "" {
		return false
	}
	k, e := x509.ParsePKIXPublicKey(b.Bytes)
	key, ok := k.(*rsa.PublicKey)
	return e == nil && ok && key.N.BitLen() >= 2048 && key.N.BitLen() <= 4096 && key.E == 65537
}

// Consistent merchant -> user -> original session locks. Revocation, password
// changes, merchant disablement and concurrent rotations serialize with commit.
func merchantRSAStateTx(ctx context.Context, tx pgx.Tx, a MerchantRSAActor, write bool) (MerchantRSAState, string, error) {
	var out MerchantRSAState
	if a.User.Role != "merchant" || a.User.ID <= 0 || a.User.AuthVersion < 1 || a.User.Username == "" || len(a.Token) != 43 || len(a.CSRF) != 43 {
		return out, "", ErrMerchantRSAActor
	}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	e := tx.QueryRow(ctx, "SELECT signature_algorithm,COALESCE(rsa_public_key,'') FROM merchants WHERE id=$1 AND enabled"+lock, a.User.ID).Scan(&out.Algorithm, &out.PublicKey)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			e = ErrMerchantRSAActor
		}
		return out, "", e
	}
	if out.Algorithm != "MD5" && out.Algorithm != "RSA" {
		return out, "", ErrMerchantRSAActor
	}
	var hash string
	e = tx.QueryRow(ctx, `SELECT password_hash FROM console_users WHERE username=$1 AND role='merchant' AND subject_id=$2 AND auth_version=$3 AND enabled FOR SHARE`, a.User.Username, a.User.ID, a.User.AuthVersion).Scan(&hash)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			e = ErrMerchantRSAActor
		}
		return out, "", e
	}
	digest := sha256.Sum256([]byte(a.Token))
	var csrf string
	var alive bool
	e = tx.QueryRow(ctx, `SELECT csrf,expires_at>clock_timestamp() FROM console_sessions WHERE token_hash=$1 AND username=$2 AND role='merchant' AND subject_id=$3 AND auth_version=$4 FOR SHARE`, digest[:], a.User.Username, a.User.ID, a.User.AuthVersion).Scan(&csrf, &alive)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			e = ErrMerchantRSAActor
		}
		return out, "", e
	}
	var delegated bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM console_session_issuers WHERE token_hash=$1)`, digest[:]).Scan(&delegated); e != nil {
		return out, "", e
	}
	if !alive || delegated || subtle.ConstantTimeCompare([]byte(csrf), []byte(a.CSRF)) != 1 {
		return out, "", ErrMerchantRSAActor
	}
	out.Fingerprint = MerchantRSAFingerprint(out.Algorithm, out.PublicKey)
	return out, hash, nil
}
func (s *Store) MerchantRSAState(ctx context.Context, a MerchantRSAActor) (MerchantRSAState, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return MerchantRSAState{}, e
	}
	defer tx.Rollback(ctx)
	state, _, e := merchantRSAStateTx(ctx, tx, a, false)
	if e != nil {
		return MerchantRSAState{}, e
	}
	return state, tx.Commit(ctx)
}
func merchantRSACheck(state MerchantRSAState, hash, password, expected string) error {
	if len(expected) != 64 || subtle.ConstantTimeCompare([]byte(expected), []byte(state.Fingerprint)) != 1 {
		return ErrMerchantRSAConflict
	}
	if !identity.CheckPassword(hash, password) {
		return ErrMerchantRSAPassword
	}
	return nil
}

// Preflight performs password and durable actor verification before expensive
// generation. It is not authorization for the subsequent atomic replacement.
func (s *Store) CheckMerchantRSARotation(ctx context.Context, a MerchantRSAActor, password, expected string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	state, hash, e := merchantRSAStateTx(ctx, tx, a, false)
	if e != nil {
		return e
	}
	if e = merchantRSACheck(state, hash, password, expected); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// Only the public key enters persistence. The private key never enters Store.
func (s *Store) RotateMerchantRSA(ctx context.Context, a MerchantRSAActor, password, expected, public string) (MerchantRSAState, error) {
	var empty MerchantRSAState
	if !validMerchantRSAPublicKey(public) {
		return empty, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	state, hash, e := merchantRSAStateTx(ctx, tx, a, true)
	if e != nil {
		return empty, e
	}
	if e = merchantRSACheck(state, hash, password, expected); e != nil {
		return empty, e
	}
	// Argon verification can cross the expiry boundary; recheck the durable
	// session using the database clock immediately before the mutation.
	if _, _, e = merchantRSAStateTx(ctx, tx, a, true); e != nil {
		return empty, e
	}
	if public == state.PublicKey {
		return empty, ErrInvalid
	}
	if _, e = tx.Exec(ctx, `UPDATE merchants SET rsa_public_key=$2,config_revision=config_revision+1 WHERE id=$1`, a.User.ID, public); e != nil {
		return empty, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,$2,'account',$1,'rsa-key-rotate','{}')`, a.User.ID, a.User.Username); e != nil {
		return empty, e
	}
	state.PublicKey = public
	state.Fingerprint = MerchantRSAFingerprint(state.Algorithm, public)
	if e = tx.Commit(ctx); e != nil {
		return empty, e
	}
	return state, nil
}
