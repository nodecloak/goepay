// Package channelconfig loads authenticated, encrypted imports from legacy installations.
package channelconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/hkdf"
	"io"
	"os"
)

const importContext = "goepay:imported-channels:v1"

var ErrImport = errors.New("invalid encrypted channel import")

type Channel struct {
	ID     int64  `json:"id"`
	Plugin string `json:"plugin"`
	// Collection mode is distinct from the provider account/agent mode.
	Mode         *int              `json:"mode,omitempty"`
	AppTypes     []string          `json:"app_types"`
	Config       map[string]string `json:"config"`
	Certificates map[string]string `json:"certificates"`
}
type Import struct {
	PaymentSubchannelTemplates       []PaymentSubchannelTemplate           `json:"payment_subchannel_templates,omitempty"`
	Version                          int                                   `json:"version"`
	Channels                         []Channel                             `json:"channels"`
	IdentityProfiles                 []IdentityProfileSpec                 `json:"identity_profiles,omitempty"`
	ApplicationAuthorizationProfiles []ApplicationAuthorizationProfileSpec `json:"application_authorization_profiles,omitempty"`
}

// Managed origin markers are minted only by the runtime vault overlay. Plain
// and encrypted external imports must never impersonate that namespace.
func (imp *Import) UnmarshalJSON(raw []byte) error {
	type plain Import
	var value plain
	if json.Unmarshal(raw, &value) != nil {
		return ErrImport
	}
	for _, p := range value.IdentityProfiles {
		if p.ManagedVaultFingerprint != "" {
			return ErrImport
		}
	}
	*imp = Import(value)
	return nil
}

func Load(path string, master []byte) (Import, error) {
	f, e := os.Open(path)
	if e != nil {
		return Import{}, ErrImport
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 4<<20))
	if e != nil {
		return Import{}, ErrImport
	}
	return Decrypt(b, master)
}

// Encrypt seals a plaintext channel import for GOEPAY_CHANNEL_CONFIG_FILE with
// the same envelope Decrypt reads: AES-256-GCM under an HKDF-SHA256 key derived
// from the master key, authenticated with the import context. The sealed bytes
// are decrypted again before they are returned, so an import that startup
// would refuse is never produced.
func Encrypt(imp Import, master []byte) ([]byte, error) {
	if len(master) != 32 {
		return nil, ErrImport
	}
	plain, e := json.Marshal(imp)
	if e != nil {
		return nil, ErrImport
	}
	g, e := importCipher(master)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, ErrImport
	}
	sealed := g.Seal(nil, nonce, plain, []byte(importContext))
	split := len(sealed) - g.Overhead()
	encode := base64.StdEncoding.EncodeToString
	out, e := json.Marshal(struct {
		Version    int    `json:"version"`
		AAD        string `json:"aad"`
		Nonce      string `json:"nonce"`
		Ciphertext string `json:"ciphertext"`
		Tag        string `json:"tag"`
	}{1, importContext, encode(nonce), encode(sealed[:split]), encode(sealed[split:])})
	if e != nil {
		return nil, ErrImport
	}
	if _, e = Decrypt(out, master); e != nil {
		return nil, e
	}
	return out, nil
}

func importCipher(master []byte) (cipher.AEAD, error) {
	key := make([]byte, 32)
	if _, e := io.ReadFull(hkdf.New(sha256.New, master, nil, []byte(importContext)), key); e != nil {
		return nil, ErrImport
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, ErrImport
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return nil, ErrImport
	}
	return g, nil
}

func Decrypt(raw, master []byte) (Import, error) {
	var out Import
	if len(master) != 32 || len(raw) > 4<<20 {
		return out, ErrImport
	}
	var envelope struct {
		Version    int    `json:"version"`
		AAD        string `json:"aad"`
		Nonce      string `json:"nonce"`
		Ciphertext string `json:"ciphertext"`
		Tag        string `json:"tag"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Version != 1 || envelope.AAD != importContext {
		return out, ErrImport
	}
	decode := base64.StdEncoding.DecodeString
	nonce, e := decode(envelope.Nonce)
	if e != nil {
		return out, ErrImport
	}
	body, e := decode(envelope.Ciphertext)
	if e != nil {
		return out, ErrImport
	}
	tag, e := decode(envelope.Tag)
	if e != nil || len(tag) != 16 {
		return out, ErrImport
	}
	g, e := importCipher(master)
	if e != nil || len(nonce) != g.NonceSize() {
		return out, ErrImport
	}
	plain, e := g.Open(nil, nonce, append(body, tag...), []byte(importContext))
	if e != nil {
		return out, ErrImport
	}
	if json.Unmarshal(plain, &out) != nil || out.Version != 1 || len(out.Channels) > 1000 {
		return Import{}, ErrImport
	}
	seen := map[int64]bool{}
	for _, c := range out.Channels {
		if c.ID <= 0 || seen[c.ID] || !KnownChannelPlugin(c.Plugin) || c.Config == nil || !supportedCollectionMode(c) {
			return Import{}, ErrImport
		}
		seen[c.ID] = true
	}
	return out, nil
}
