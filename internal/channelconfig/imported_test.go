package channelconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"golang.org/x/crypto/hkdf"
	"io"
	"testing"
)

func fixture(t *testing.T, key []byte) []byte {
	t.Helper()
	derived := make([]byte, 32)
	io.ReadFull(hkdf.New(sha256.New, key, nil, []byte(importContext)), derived)
	block, _ := aes.NewCipher(derived)
	g, _ := cipher.NewGCM(block)
	nonce := make([]byte, 12)
	plain := []byte(`{"version":1,"channels":[{"id":1,"plugin":"alipay","config":{"appid":"test","appsecret":"secret"},"certificates":{}}]}`)
	sealed := g.Seal(nil, nonce, plain, []byte(importContext))
	b, _ := json.Marshal(map[string]any{"version": 1, "aad": importContext, "nonce": base64.StdEncoding.EncodeToString(nonce), "ciphertext": base64.StdEncoding.EncodeToString(sealed[:len(sealed)-16]), "tag": base64.StdEncoding.EncodeToString(sealed[len(sealed)-16:])})
	return b
}
func TestDecryptRejectsWrongMasterAndTampering(t *testing.T) {
	key := make([]byte, 32)
	raw := fixture(t, key)
	v, e := Decrypt(raw, key)
	if e != nil || len(v.Channels) != 1 || v.Channels[0].Config["appid"] != "test" {
		t.Fatalf("decrypt failed: %v", e)
	}
	wrong := make([]byte, 32)
	wrong[0] = 1
	if _, e = Decrypt(raw, wrong); e == nil {
		t.Fatal("wrong key accepted")
	}
	var envelope map[string]any
	json.Unmarshal(raw, &envelope)
	envelope["aad"] = "another-context"
	bad, _ := json.Marshal(envelope)
	if _, e = Decrypt(bad, key); e == nil {
		t.Fatal("wrong context accepted")
	}
	if _, e = Decrypt(raw, key[:8]); e == nil {
		t.Fatal("short master accepted")
	}
}
