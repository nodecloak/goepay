package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/compat"
)

func TestMerchantCreationDefaultsToBothWithoutChangingExplicitSignatureChoices(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	both, only := 0, 1
	for i, tc := range []struct {
		name, algorithm, public string
		policy                  *int
		create                  string
		want                    *int
		md5, rsa                bool
	}{
		{"default_store", "", "", nil, "store", &both, true, false},
		{"default_atomic", "", "", nil, "atomic", &both, true, false},
		{"default_caller_transaction", "", "", nil, "tx", &both, true, false},
		{"default_with_rsa_credential", "", public, nil, "store", &both, true, true},
		{"explicit_md5", "MD5", public, nil, "store", nil, true, false},
		{"explicit_rsa", "RSA", public, nil, "store", nil, false, true},
		{"explicit_both", "MD5", public, &both, "store", &both, true, true},
		{"explicit_rsa_only", "MD5", public, &only, "store", &only, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Merchant{ID: int64(1000 + i), Name: tc.name, APIKey: "synthetic-signature-default-key", SignatureAlgorithm: tc.algorithm, RSAPublicKey: tc.public, Keytype: tc.policy}
			var err error
			switch tc.create {
			case "atomic":
				err = s.CreateMerchantAtomic(ctx, m, nil)
			case "tx":
				var tx pgx.Tx
				tx, err = s.pool.Begin(ctx)
				if err == nil {
					defer tx.Rollback(ctx)
					err = s.CreateMerchantInTx(ctx, tx, m)
					if err == nil {
						err = tx.Commit(ctx)
					}
				}
			default:
				err = s.CreateMerchant(ctx, m)
			}
			if err != nil {
				t.Fatal(err)
			}
			stored, err := s.Merchant(ctx, m.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (tc.want == nil) != (stored.Keytype == nil) || tc.want != nil && *tc.want != *stored.Keytype {
				t.Fatalf("stored signing policy=%v want=%v", stored.Keytype, tc.want)
			}
			if stored.RSAPublicKey != tc.public || stored.APIKey != m.APIKey {
				t.Fatal("creation changed supplied credentials")
			}
			var parsed *rsa.PublicKey
			if stored.RSAPublicKey != "" {
				parsed, err = compat.ParsePublicKey(stored.RSAPublicKey)
				if err != nil {
					t.Fatal(err)
				}
			}
			credentials := compat.Credentials{Algorithm: stored.SignatureAlgorithm, Keytype: stored.Keytype, MD5Key: stored.APIKey, PublicKey: parsed}
			now := time.Unix(1700000000, 0)
			for _, algorithm := range []string{"MD5", "RSA"} {
				params := compat.Params{"pid": fmt.Sprint(m.ID), "sign_type": algorithm, "timestamp": "1700000000"}
				want := tc.md5
				if algorithm == "MD5" {
					params["sign"] = compat.SignMD5(params, stored.APIKey)
				} else {
					want = tc.rsa
					params["sign"], err = compat.SignRSA(params, key)
					if err != nil {
						t.Fatal(err)
					}
				}
				if got := compat.Verify(params, credentials, now) == nil; got != want {
					t.Fatalf("algorithm=%s verified=%v want=%v", algorithm, got, want)
				}
			}
		})
	}
	// New defaults never update an existing strict merchant or the SQL default.
	var policy *int
	if err := s.pool.QueryRow(ctx, `SELECT keytype FROM merchants WHERE name='explicit_md5'`).Scan(&policy); err != nil || policy != nil {
		t.Fatal("existing explicit policy changed", err)
	}
	var defaultExpression *string
	if err := s.pool.QueryRow(ctx, `SELECT column_default FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='merchants' AND column_name='keytype'`).Scan(&defaultExpression); err != nil || defaultExpression != nil {
		t.Fatal("historical raw-insert default changed", defaultExpression, err)
	}
}

func TestMerchantCreationRejectsInvalidOrUnprovisionedExplicitSignaturePolicy(t *testing.T) {
	s := testStore(t)
	for i, policy := range []int{-1, 2, 1} {
		m := Merchant{ID: int64(1100 + i), Name: "invalid explicit policy", APIKey: "synthetic-signature-invalid-key", Keytype: &policy}
		if err := s.CreateMerchant(t.Context(), m); !errors.Is(err, ErrInvalid) {
			t.Fatalf("policy=%d accepted or wrong error: %v", policy, err)
		}
	}
	var count int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM merchants`).Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid policy persisted", count, err)
	}
}
