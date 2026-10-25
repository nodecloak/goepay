package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/payment"
	"goepay/internal/server"
	"golang.org/x/crypto/hkdf"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredProvidersExplicitFileAndDisabledWrites(t *testing.T) {
	master := bytes.Repeat([]byte{2}, 32)
	plain, _ := json.Marshal(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "test-app", "appmchid": "test-mch", "appkey": "0123456789abcdef0123456789abcdef"}}}})
	derived := make([]byte, 32)
	io.ReadFull(hkdf.New(sha256.New, master, nil, []byte("goepay:imported-channels:v1")), derived)
	block, _ := aes.NewCipher(derived)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, 12)
	rand.Read(nonce)
	sealed := gcm.Seal(nil, nonce, plain, []byte("goepay:imported-channels:v1"))
	envelope, _ := json.Marshal(map[string]any{"version": 1, "aad": "goepay:imported-channels:v1", "nonce": base64.StdEncoding.EncodeToString(nonce), "ciphertext": base64.StdEncoding.EncodeToString(sealed[:len(sealed)-16]), "tag": base64.StdEncoding.EncodeToString(sealed[len(sealed)-16:])})
	path := filepath.Join(t.TempDir(), "test-config.enc.json")
	os.WriteFile(path, envelope, 0600)
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", path)
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1000":[7]}`)
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "")
	app := &server.Server{}
	if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e != nil {
		t.Fatal(e)
	}
	if app.ProviderFlow == nil || app.ProviderFlow.AllowLive || app.Simulation != nil {
		t.Fatal("configured provider unexpectedly write-enabled or simulationmixed")
	}
	if _, e := app.ProviderFlow.Submit(context.Background(), "test", 7, "native", "192.0.2.1", ""); !errors.Is(e, payment.ErrLiveDisabled) {
		t.Fatal("write wasn't blocked")
	}
	if _, e := app.ProviderFlow.Providers[7].Create(context.Background(), adapter.CreateRequest{}); !errors.Is(e, channelconfig.ErrLiveDisabled) {
		t.Fatal("provider wrapper didn't block write")
	}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", "")
	if e := configureProviders(&server.Server{}, "configured", master, "https://pay.example.invalid"); e == nil {
		t.Fatal("auto discovered file instead of requiring explicit path")
	}
}
func TestConfiguredMerchantAllowlistStrict(t *testing.T) {
	providers := map[int64]adapter.Provider{7: nil}
	for _, raw := range []string{`{"1000":[7],"1000":[7]}`, `{"1000":[7,7]}`, `{"0":[7]}`, `{"1000":[8]}`, `{"01":[7]}`, `{"1000":[7]} trailing`} {
		if _, e := configuredAllowlist(raw, providers); e == nil {
			t.Fatal("ambiguous/unknown allowlist accepted")
		}
	}
}
