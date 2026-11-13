package cryptosigner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"goepay/internal/cryptochain"
)

func TestProvisionGeneratesPrivateFileAndOnlyPublicMatchingAddresses(t *testing.T) {
	dir := provisionPrivateTestDir(t)
	got, err := ProvisionKey(dir, "invoice-wallet")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "invoice-wallet.key")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes.TrimSpace(raw)) != 64 {
		t.Fatal("missing local private key file")
	}
	publicJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(publicJSON, bytes.TrimSpace(raw)) || strings.Contains(string(publicJSON), "private") {
		t.Fatal("private key leaked into public result")
	}
	keys, err := NewFileKeys(map[string]string{got.KeyID: path})
	if err != nil {
		t.Fatal(err)
	}
	digest := keccak([]byte("local provisioning signature test"))
	sig, err := keys.SignDigest(context.Background(), got.KeyID, digest)
	if err != nil {
		t.Fatal(err)
	}
	pub, _, err := ecdsa.RecoverCompact(append([]byte{27 + sig[64]}, sig[:64]...), digest)
	if err != nil {
		t.Fatal(err)
	}
	if !publicKeyMatches(pub.SerializeUncompressed(), cryptochain.Ethereum, got.EthereumAddress) || !publicKeyMatches(pub.SerializeUncompressed(), cryptochain.Tron, got.TRONAddress) {
		t.Fatal("published address does not own the generated signing key")
	}
	if _, err = ProvisionKey(dir, "invoice-wallet"); err == nil {
		t.Fatal("existing wallet key overwritten")
	}
	again, _ := os.ReadFile(path)
	if !bytes.Equal(raw, again) {
		t.Fatal("key changed on rejected overwrite")
	}
}

func TestProvisionRejectsTraversalSymlinkAndBroadDirectory(t *testing.T) {
	dir := provisionPrivateTestDir(t)
	for _, id := range []string{"", "../outside", "a/b", `a\b`, ".hidden", "has space", "NUL", "con", "COM1", "LPT9"} {
		if _, err := ProvisionKey(dir, id); err == nil {
			t.Fatalf("unsafe key ID accepted: %q", id)
		}
	}
	broad := t.TempDir()
	provisionBroadenTestDir(t, broad)
	if _, err := ProvisionKey(broad, "unsafe-parent"); err == nil {
		t.Fatal("broad directory accepted")
	}
	if _, err := os.Stat(filepath.Join(broad, "unsafe-parent.key")); !os.IsNotExist(err) {
		t.Fatal("created secret in rejected directory")
	}
	target := filepath.Join(dir, "existing.key")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked.key")
	if err := os.Symlink(target, link); err == nil {
		if _, err = ProvisionKey(dir, "linked"); err == nil {
			t.Fatal("symlink key replaced")
		}
		b, _ := os.ReadFile(target)
		if string(b) != "keep" {
			t.Fatal("symlink target changed")
		}
	} else {
		t.Log("symlink creation unavailable on this host; exclusive-create rejection remains tested")
	}
}
