package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"goepay/internal/identity"
	"testing"
)

func TestMerchantRSAFingerprintAndPublicKey(t *testing.T) {
	a := MerchantRSAFingerprint("MD5", "")
	if len(a) != 64 || a == MerchantRSAFingerprint("RSA", "") || a == MerchantRSAFingerprint("MD5", "x") {
		t.Fatal("fingerprint must bind algorithm and exact key")
	}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	der, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	valid := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	for _, raw := range []string{"", "not a key", "junk\n" + valid, valid + "junk", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)}))} {
		if validMerchantRSAPublicKey(raw) {
			t.Fatal("accepted non-PKIX public key")
		}
	}
	if !validMerchantRSAPublicKey(valid) {
		t.Fatal("rejected PKIX public key")
	}
}

func TestMerchantRSADurableActorAndMD5Standby(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 1, Name: "rsa owner", APIKey: "md5-unchanged", SignatureAlgorithm: "MD5"}); e != nil {
		t.Fatal(e)
	}
	hash, e := identity.HashPassword("rsa-owner-password-123")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('rsa-owner','merchant',1,'Owner',$1)`, hash); e != nil {
		t.Fatal(e)
	}
	sessions := identity.NewPersistentSessions(s.pool, false)
	actor := MerchantRSAActor{User: identity.User{Username: "rsa-owner", ID: 1, Role: "merchant", AuthVersion: 1}}
	actor.Token, actor.CSRF, e = sessions.Create(actor.User)
	if e != nil {
		t.Fatal(e)
	}
	state, e := s.MerchantRSAState(ctx, actor)
	if e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*MerchantRSAActor){func(a *MerchantRSAActor) { a.User.ID = 2 }, func(a *MerchantRSAActor) { a.User.Role = "admin" }, func(a *MerchantRSAActor) { a.User.AuthVersion++ }, func(a *MerchantRSAActor) { a.User.Username = "other" }, func(a *MerchantRSAActor) { a.Token = "!" + a.Token[1:] }, func(a *MerchantRSAActor) { a.CSRF = "!" + a.CSRF[1:] }} {
		bad := actor
		mutate(&bad)
		if _, e = s.MerchantRSAState(ctx, bad); !errors.Is(e, ErrMerchantRSAActor) {
			t.Fatal("forged actor accepted", e)
		}
	}
	if e = s.CheckMerchantRSARotation(ctx, actor, "wrong", state.Fingerprint); !errors.Is(e, ErrMerchantRSAPassword) {
		t.Fatal("wrong password", e)
	}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	// Preflight does not authorize subsequent I/O/commit after source changes.
	for _, tc := range []struct{ name, change, restore string }{
		{"merchant-off", `UPDATE merchants SET enabled=false WHERE id=1`, `UPDATE merchants SET enabled=true WHERE id=1`},
		{"user-off", `UPDATE console_users SET enabled=false WHERE username='rsa-owner'`, `UPDATE console_users SET enabled=true WHERE username='rsa-owner'`},
		{"session-revoked", `DELETE FROM console_sessions WHERE username='rsa-owner'`, ``},
		{"session-expired", `UPDATE console_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE username='rsa-owner'`, ``},
		{"delegated", `INSERT INTO console_session_issuers(token_hash,issuer_session_hash,issuer_username,issuer_id,issuer_auth_version) SELECT token_hash,token_hash,'issuer',9,1 FROM console_sessions WHERE username='rsa-owner'`, `DELETE FROM console_session_issuers`},
		{"auth-version", `UPDATE console_users SET auth_version=auth_version+1 WHERE username='rsa-owner'`, ``},
		{"password-changed", `UPDATE console_users SET password_hash=password_hash||'invalid' WHERE username='rsa-owner'`, ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if e = s.CheckMerchantRSARotation(ctx, actor, "rsa-owner-password-123", state.Fingerprint); e != nil {
				t.Fatal(e)
			}
			if _, e = s.pool.Exec(ctx, tc.change); e != nil {
				t.Fatal(e)
			}
			if _, e = s.RotateMerchantRSA(ctx, actor, "rsa-owner-password-123", state.Fingerprint, public); !errors.Is(e, ErrMerchantRSAActor) {
				t.Fatal("stale preflight authorized commit", e)
			}
			if tc.restore != "" {
				if _, e = s.pool.Exec(ctx, tc.restore); e != nil {
					t.Fatal(e)
				}
			}
			if tc.name == "password-changed" {
				if _, e = s.pool.Exec(ctx, `UPDATE console_users SET password_hash=$1 WHERE username='rsa-owner'`, hash); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = s.pool.Exec(ctx, `DELETE FROM console_sessions WHERE username='rsa-owner'`); e != nil {
				t.Fatal(e)
			}
			if e = s.pool.QueryRow(ctx, `SELECT auth_version FROM console_users WHERE username='rsa-owner'`).Scan(&actor.User.AuthVersion); e != nil {
				t.Fatal(e)
			}
			actor.Token, actor.CSRF, e = sessions.Create(actor.User)
			if e != nil {
				t.Fatal(e)
			}
		})
	}
	result, e := s.RotateMerchantRSA(ctx, actor, "rsa-owner-password-123", state.Fingerprint, public)
	if e != nil {
		t.Fatal(e)
	}
	m, e := s.Merchant(ctx, 1)
	if e != nil || result.Algorithm != "MD5" || m.SignatureAlgorithm != "MD5" || m.APIKey != "md5-unchanged" || m.RSAPublicKey != public {
		t.Fatal("standby generation changed MD5 policy", e)
	}
	if _, e = s.RotateMerchantRSA(ctx, actor, "rsa-owner-password-123", state.Fingerprint, public); !errors.Is(e, ErrMerchantRSAConflict) {
		t.Fatal("old CAS replay", e)
	}
}
