package cryptosigner

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"goepay/internal/cryptochain"
)

// ProvisionedKey contains only public data safe to import into the API address
// pool. Treasury key bytes are written only to directory/keyID.key.
type ProvisionedKey struct {
	KeyID           string `json:"key_id"`
	EthereumAddress string `json:"ethereum_address"`
	TRONAddress     string `json:"tron_address"`
}

// ProvisionKey generates one cryptographically random local key. The directory
// must already exist with owner-only access (Windows: current user, SYSTEM and
// Administrators only). Existing files and symlink paths are never replaced.
func ProvisionKey(directory, keyID string) (ProvisionedKey, error) {
	var result ProvisionedKey
	if directory == "" {
		return result, fmt.Errorf("explicit private key directory required")
	}
	if len(keyID) == 0 || len(keyID) > 64 {
		return result, fmt.Errorf("key ID must contain 1 to 64 letters, digits, '-' or '_'")
	}
	for _, c := range keyID {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return result, fmt.Errorf("unsafe key ID")
	}
	upper := strings.ToUpper(keyID)
	if upper == "CON" || upper == "PRN" || upper == "AUX" || upper == "NUL" || (len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) && upper[3] >= '1' && upper[3] <= '9') {
		return result, fmt.Errorf("reserved device name cannot be a key ID")
	}
	dir, err := filepath.Abs(directory)
	if err != nil {
		return result, err
	}
	if err = rejectSymlinkPath(dir); err != nil {
		return result, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return result, fmt.Errorf("key directory must already exist")
	}
	if err = provisionPrivatePermissions(dir); err != nil {
		return result, err
	}
	path := filepath.Join(dir, keyID+".key")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, fmt.Errorf("create exclusive key file: %w", err)
	}
	keep := false
	defer func() {
		f.Close()
		if !keep {
			os.Remove(path)
		}
	}()
	if err = provisionPrivatePermissions(path); err != nil {
		return result, err
	}
	key, err := secp256k1.GeneratePrivateKeyFromRand(rand.Reader)
	if err != nil {
		return result, fmt.Errorf("generate key: %w", err)
	}
	defer key.Zero()
	raw := key.Serialize()
	defer clear(raw)
	encoded := make([]byte, hex.EncodedLen(len(raw))+1)
	defer clear(encoded)
	hex.Encode(encoded, raw)
	encoded[len(encoded)-1] = '\n'
	n, err := f.Write(encoded)
	if err == nil && n != len(encoded) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		return result, fmt.Errorf("persist local key: %w", err)
	}
	if err = f.Close(); err != nil {
		return result, err
	}
	if err = provisionSyncDirectory(dir); err != nil {
		return result, err
	}
	digest := keccak(key.PubKey().SerializeUncompressed()[1:])
	body := hex.EncodeToString(digest[12:])
	tron, err := cryptochain.NormalizeAddress(cryptochain.Tron, "41"+body)
	if err != nil {
		return result, err
	}
	result = ProvisionedKey{KeyID: keyID, EthereumAddress: "0x" + body, TRONAddress: tron}
	keep = true
	return result, nil
}

func rejectSymlinkPath(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("key directory must not contain symlinks")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}
