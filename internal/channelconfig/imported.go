// Package channelconfig loads authenticated, encrypted imports from legacy installations.
package channelconfig

import (
	"crypto/aes"
	"crypto/cipher"
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
	key := make([]byte, 32)
	if _, e = io.ReadFull(hkdf.New(sha256.New, master, nil, []byte(importContext)), key); e != nil {
		return out, ErrImport
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return out, ErrImport
	}
	g, e := cipher.NewGCM(block)
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
		if c.ID <= 0 || seen[c.ID] || (c.Plugin != "wxpay" && c.Plugin != "alipay") || c.Config == nil || !supportedCollectionMode(c) {
			return Import{}, ErrImport
		}
		seen[c.ID] = true
	}
	return out, nil
}
