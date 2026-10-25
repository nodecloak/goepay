package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/payment"
	"golang.org/x/crypto/hkdf"
)

func TestReconciliationWorkerLoadsOnlyExplicitReadOnlyConfiguration(t *testing.T) {
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", "")
	t.Setenv("GOEPAY_ENABLE_PROVIDER_QUERIES", "")
	t.Setenv("GOEPAY_PROVIDER_MODE", "simulation")
	if f, e := configuredReadOnlyFlow(nil); e != nil || f != nil {
		t.Fatal(f, e)
	}
	t.Setenv("GOEPAY_PROVIDER_MODE", "configured")
	if f, e := configuredReadOnlyFlow(nil); e != nil || f != nil {
		t.Fatal("queries should require explicit opt-in", f, e)
	}
	t.Setenv("GOEPAY_ENABLE_PROVIDER_QUERIES", "true")
	if _, e := configuredReadOnlyFlow(nil); e == nil {
		t.Fatal("missing path accepted")
	}
	master := bytes.Repeat([]byte{3}, 32)
	t.Setenv("GOEPAY_MASTER_KEY", "0303030303030303030303030303030303030303030303030303030303030303")
	plain, _ := json.Marshal(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "synthetic", "appmchid": "synthetic", "appkey": "0123456789abcdef0123456789abcdef"}}}})
	key := make([]byte, 32)
	io.ReadFull(hkdf.New(sha256.New, master, nil, []byte("goepay:imported-channels:v1")), key)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := bytes.Repeat([]byte{4}, 12)
	sealed := gcm.Seal(nil, nonce, plain, []byte("goepay:imported-channels:v1"))
	raw, _ := json.Marshal(map[string]any{"version": 1, "aad": "goepay:imported-channels:v1", "nonce": base64.StdEncoding.EncodeToString(nonce), "ciphertext": base64.StdEncoding.EncodeToString(sealed[:len(sealed)-16]), "tag": base64.StdEncoding.EncodeToString(sealed[len(sealed)-16:])})
	path := filepath.Join(t.TempDir(), "synthetic.enc.json")
	os.WriteFile(path, raw, 0600)
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", path)
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "true") // Worker must ignore this API-only opt-in.
	flow, e := configuredReadOnlyFlow(nil)
	if e != nil || flow == nil || flow.AllowLive {
		t.Fatal(flow, e)
	}
	if _, e = flow.Submit(context.Background(), "irrelevant", 7, "native", "", ""); !errors.Is(e, payment.ErrLiveDisabled) {
		t.Fatal(e)
	}
	if _, e = flow.Providers[7].Create(context.Background(), adapter.CreateRequest{}); !errors.Is(e, channelconfig.ErrLiveDisabled) {
		t.Fatal(e)
	}
	if _, e = flow.Providers[7].Refund(context.Background(), adapter.RefundRequest{}); !errors.Is(e, channelconfig.ErrLiveDisabled) {
		t.Fatal(e)
	}
}
