package payment

import (
	"context"
	"crypto/hmac"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"goepay/internal/identity"
)

var ErrMerchantKeyLogin = errors.New("merchant key login rejected")

// This grant is process-private and cannot be supplied as merchant JSON. MFA
// is verified by the server before completion; completion rechecks its exact
// account version, key/config and policy under canonical durable locks.
type MerchantKeyLoginGrant struct {
	user                                         identity.User
	merchantID, merchantRevision, policyRevision int64
	keyCipher, policyProof                       []byte
	platformFingerprint                          string
	configured, verified                         bool
	until                                        time.Time
}

func (g MerchantKeyLoginGrant) User() identity.User { return g.user }
func merchantKeyLoginUserTx(ctx context.Context, tx pgx.Tx, mid int64) (identity.User, error) {
	var out identity.User
	rows, e := tx.Query(ctx, `SELECT username,subject_id,display_name,auth_version,admin_class FROM console_users WHERE role='merchant' AND subject_id=$1 AND enabled ORDER BY username FOR SHARE`, mid)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > 1 {
			return identity.User{}, ErrMerchantKeyLogin
		}
		if e = rows.Scan(&out.Username, &out.ID, &out.Name, &out.AuthVersion, &out.AdminClass); e != nil {
			return out, e
		}
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if count != 1 || out.AuthVersion <= 0 {
		return out, ErrMerchantKeyLogin
	}
	out.Role = "merchant"
	// The durable column defaults to operator for every account role. It is
	// administrator-only metadata and must not enter a merchant login DTO.
	out.AdminClass = ""
	return out, nil
}
func (s *Store) PrepareMerchantKeyLogin(ctx context.Context, mid int64, key string) (MerchantKeyLoginGrant, error) {
	var grant MerchantKeyLoginGrant
	if mid <= 0 || key == "" || len(key) > 512 {
		return grant, ErrMerchantKeyLogin
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return grant, e
	}
	defer tx.Rollback(ctx)
	m, e := s.merchantAPIAccountTx(ctx, tx, mid, false)
	if errors.Is(e, ErrNotFound) {
		return grant, ErrMerchantKeyLogin
	}
	if e != nil {
		return grant, e
	}
	if !m.Enabled {
		return grant, ErrMerchantKeyLogin
	}
	platform, e := s.platformAccessTx(ctx, tx, false)
	if e != nil {
		return grant, e
	}
	if platform.CloseKeyLogin != "0" {
		return grant, ErrMerchantKeyLogin
	}
	p, configured, e := s.merchantAPIPolicyTx(ctx, tx, m, false)
	if e != nil {
		return grant, e
	}
	grant.user, e = merchantKeyLoginUserTx(ctx, tx, mid)
	if e != nil {
		return grant, e
	}
	if p.KeyLogin != 1 || s.aead == nil || len(m.KeyCipher) < s.aead.NonceSize() {
		return grant, ErrMerchantKeyLogin
	}
	n := s.aead.NonceSize()
	secret, e := s.aead.Open(nil, m.KeyCipher[:n], m.KeyCipher[n:], []byte(fmt.Sprint(mid)))
	if e != nil {
		return grant, e
	}
	if subtle.ConstantTimeCompare(secret, []byte(key)) != 1 {
		return grant, ErrMerchantKeyLogin
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return grant, e
	}
	grant.merchantID, grant.merchantRevision, grant.policyRevision = mid, m.Revision, p.Revision
	grant.keyCipher, grant.policyProof = m.KeyCipher, p.Proof
	grant.configured, grant.verified = configured, true
	grant.platformFingerprint = platform.Fingerprint
	grant.until = now.Add(2 * time.Minute)
	return grant, tx.Commit(ctx)
}
func (s *Store) CompleteMerchantKeyLogin(ctx context.Context, g MerchantKeyLoginGrant, sessions *identity.Sessions) (identity.User, string, string, error) {
	fail := func(e error) (identity.User, string, string, error) { return identity.User{}, "", "", e }
	if !g.verified || g.merchantID <= 0 || g.until.IsZero() {
		return fail(ErrMerchantKeyLogin)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return fail(e)
	}
	defer tx.Rollback(ctx)
	m, e := s.merchantAPIAccountTx(ctx, tx, g.merchantID, false)
	if e != nil {
		return fail(e)
	}
	platform, e := s.platformAccessTx(ctx, tx, false)
	if e != nil {
		return fail(e)
	}
	if platform.CloseKeyLogin != "0" || !hmac.Equal([]byte(platform.Fingerprint), []byte(g.platformFingerprint)) {
		return fail(ErrMerchantKeyLogin)
	}
	p, configured, e := s.merchantAPIPolicyTx(ctx, tx, m, false)
	if e != nil {
		return fail(e)
	}
	if !m.Enabled || p.KeyLogin != 1 || m.Revision != g.merchantRevision || p.Revision != g.policyRevision || configured != g.configured || !hmac.Equal(m.KeyCipher, g.keyCipher) || !hmac.Equal(p.Proof, g.policyProof) {
		return fail(ErrMerchantKeyLogin)
	}
	u, e := merchantKeyLoginUserTx(ctx, tx, g.merchantID)
	if e != nil {
		return fail(e)
	}
	if u.Username != g.user.Username || u.ID != g.user.ID || u.AuthVersion != g.user.AuthVersion {
		return fail(ErrMerchantKeyLogin)
	}
	token, csrf, e := sessions.CreateMerchantSessionTx(ctx, tx, u)
	if e != nil {
		return fail(e)
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,$2,'merchant-key-login',$1,'merchant-key-login','{"method":"merchant_key"}')`, u.ID, u.Username); e != nil {
		return fail(e)
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, u, token, csrf); e != nil {
		return fail(e)
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return fail(e)
	}
	if !now.Before(g.until) {
		return fail(ErrMerchantKeyLogin)
	}
	if e = tx.Commit(ctx); e != nil {
		return fail(e)
	}
	return u, token, csrf, nil
}
