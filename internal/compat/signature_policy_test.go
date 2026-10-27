package compat

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"
)

func TestSignature163StoredPolicyNeverComesFromRequest(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Unix(1700000000, 0)
	both, only, invalid := 0, 1, 2
	for _, tc := range []struct {
		name, algorithm string
		policy          *int
		md5, rsa        bool
	}{
		{"strict MD5", "MD5", nil, true, false}, {"strict RSA", "RSA", nil, false, true},
		{"explicit both on MD5", "MD5", &both, true, true}, {"explicit both on RSA", "RSA", &both, true, true},
		{"RSA-only on MD5", "MD5", &only, false, true}, {"RSA-only on RSA", "RSA", &only, false, true},
		{"invalid fails closed", "MD5", &invalid, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Credentials{Algorithm: tc.algorithm, Keytype: tc.policy, MD5Key: "local-synthetic163", PublicKey: &key.PublicKey}
			for _, alg := range []string{"MD5", "RSA"} {
				p := Params{"pid": "1", "sign_type": alg, "timestamp": "1700000000", "keytype": "0"}
				if alg == "MD5" {
					p["sign"] = SignMD5(p, c.MD5Key)
				} else {
					p["sign"], e = SignRSA(p, key)
					if e != nil {
						t.Fatal(e)
					}
				}
				want := tc.md5
				if alg == "RSA" {
					want = tc.rsa
				}
				if got := Verify(p, c, now) == nil; got != want {
					t.Fatal("request selected stored policy", alg, got, want)
				}
				p["sign"] = "invalid"
				if Verify(p, c, now) == nil {
					t.Fatal("mode accepted invalid signature", alg)
				}
			}
		})
	}
	c := Credentials{Algorithm: "MD5", Keytype: &both, PublicKey: &key.PublicKey}
	p := Params{"pid": "1", "sign_type": "RSA", "timestamp": "1699999699"}
	p["sign"], e = SignRSA(p, key)
	if e != nil {
		t.Fatal(e)
	}
	if Verify(p, c, now) == nil {
		t.Fatal("compatibility bypassed RSA timestamp")
	}
}
