package cryptosigner

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"goepay/internal/cryptochain"
	"golang.org/x/crypto/sha3"
)

type FileKeys struct {
	keys map[string]*secp256k1.PrivateKey
}

// NewFileKeys reads dedicated signer-owned files. Windows deployments must
// restrict NTFS ACLs to the service account; Unix permissions are enforced here.
func NewFileKeys(paths map[string]string) (*FileKeys, error) {
	k := &FileKeys{keys: map[string]*secp256k1.PrivateKey{}}
	for id, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if id == "" || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
			return nil, fmt.Errorf("signer key file must be regular and owner-only")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		raw, err := hex.DecodeString(strings.TrimSpace(string(b)))
		clear(b)
		if err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("invalid signer private key")
		}
		var scalar secp256k1.ModNScalar
		if scalar.SetByteSlice(raw) || scalar.IsZero() {
			clear(raw)
			return nil, fmt.Errorf("invalid signer private key scalar")
		}
		k.keys[id] = secp256k1.NewPrivateKey(&scalar)
		scalar.Zero()
		clear(raw)
	}
	return k, nil
}

func (k *FileKeys) PublicKey(ctx context.Context, id string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, ok := k.keys[id]
	if !ok {
		return nil, fmt.Errorf("signer key handle not found")
	}
	return key.PubKey().SerializeUncompressed(), nil
}
func (k *FileKeys) SignDigest(ctx context.Context, id string, digest []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, ok := k.keys[id]
	if !ok || len(digest) != 32 {
		return nil, fmt.Errorf("invalid key handle or digest")
	}
	compact := ecdsa.SignCompact(key, digest, false)
	recovery := compact[0] - 27
	if recovery > 1 {
		return nil, fmt.Errorf("unsupported signature recovery ID")
	}
	return append(append([]byte(nil), compact[1:]...), recovery), nil
}

func keccak(b []byte) []byte { h := sha3.NewLegacyKeccak256(); h.Write(b); return h.Sum(nil) }
func publicKeyMatches(pub []byte, chain cryptochain.Chain, address string) bool {
	key, err := secp256k1.ParsePubKey(pub)
	if err != nil {
		return false
	}
	want, err := addressBytes(chain, address)
	if err != nil {
		return false
	}
	got := keccak(key.SerializeUncompressed()[1:])[12:]
	if chain == cryptochain.Tron {
		got = append([]byte{0x41}, got...)
	}
	return bytes.Equal(got, want)
}
func verifySignature(pub, digest, sig []byte) error {
	if len(sig) != 65 || sig[64] > 1 {
		return fmt.Errorf("invalid signature")
	}
	var r, s secp256k1.ModNScalar
	if r.SetByteSlice(sig[:32]) || s.SetByteSlice(sig[32:64]) || r.IsZero() || s.IsZero() || s.IsOverHalfOrder() {
		return fmt.Errorf("invalid signature scalar")
	}
	compact := append([]byte{27 + sig[64]}, sig[:64]...)
	key, _, err := ecdsa.RecoverCompact(compact, digest)
	if err != nil || !bytes.Equal(key.SerializeUncompressed(), pub) {
		return fmt.Errorf("signature public key mismatch")
	}
	return nil
}
