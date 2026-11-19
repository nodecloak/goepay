package backoffice

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"testing"
)

func TestAdminMerchantCreationDefaultsBothAndPreservesExplicitAlgorithms(t *testing.T) {
	s := setup(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	for i, tc := range []struct{ mode, algorithm string }{{"account", ""}, {"api", ""}, {"", ""}, {"api", "MD5"}, {"api", "RSA"}} {
		body := map[string]any{"id": fmt.Sprint(2200 + i), "name": fmt.Sprintf("new-signature-%d", i)}
		if tc.mode != "" {
			body["creation_mode"] = tc.mode
		}
		if tc.mode == "account" {
			body["email"] = "default-signature@example.test"
			body["login_password"] = "synthetic-signature-password"
		}
		if tc.algorithm != "" {
			body["signature_algorithm"] = tc.algorithm
		}
		if tc.algorithm == "RSA" {
			body["rsa_public_key"] = public
		}
		raw, _ := json.Marshal(body)
		created, err := s.Create(t.Context(), createSetupSuper, "merchants", input(string(raw)))
		if err != nil {
			t.Fatal(tc, err)
		}
		m, err := s.store.Merchant(t.Context(), created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if tc.algorithm == "" {
			if m.Keytype == nil || *m.Keytype != 0 || m.RSAPublicKey != "" {
				t.Fatalf("mode=%s lacks dual default or generated an unsolicited RSA credential", tc.mode)
			}
		} else if m.Keytype != nil || m.SignatureAlgorithm != tc.algorithm {
			t.Fatalf("explicit %s policy changed", tc.algorithm)
		}
		if _, err := s.Update(t.Context(), createSetupSuper, "merchants", created.ID, input(`{"name":"renamed merchant"}`)); err != nil {
			t.Fatal(err)
		}
		updated, err := s.store.Merchant(t.Context(), created.ID)
		if err != nil || (updated.Keytype == nil) != (m.Keytype == nil) || m.Keytype != nil && *updated.Keytype != *m.Keytype {
			t.Fatal("ordinary edit changed signing policy", err)
		}
	}
}
