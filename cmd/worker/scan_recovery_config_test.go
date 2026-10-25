package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"testing"

	provider "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"golang.org/x/crypto/hkdf"
)

func recoveryWorkerConfig78(t *testing.T) {
	t.Helper()
	private, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	c := channelconfig.Channel{ID: 7, Plugin: "alipay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "app78", "appmchid": "seller78", "enable_scan_pay": "true", "appsecret": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(private)})), "appkey": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub}))}}
	plain, e := json.Marshal(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{c}})
	if e != nil {
		t.Fatal(e)
	}
	master := bytes.Repeat([]byte{3}, 32)
	key := make([]byte, 32)
	if _, e = io.ReadFull(hkdf.New(sha256.New, master, nil, []byte("goepay:imported-channels:v1")), key); e != nil {
		t.Fatal(e)
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		t.Fatal(e)
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		t.Fatal(e)
	}
	nonce := bytes.Repeat([]byte{4}, 12)
	sealed := gcm.Seal(nil, nonce, plain, []byte("goepay:imported-channels:v1"))
	raw, e := json.Marshal(map[string]any{"version": 1, "aad": "goepay:imported-channels:v1", "nonce": base64.StdEncoding.EncodeToString(nonce), "ciphertext": base64.StdEncoding.EncodeToString(sealed[:len(sealed)-16]), "tag": base64.StdEncoding.EncodeToString(sealed[len(sealed)-16:])})
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "synthetic-scan-recovery78.enc.json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", path)
	t.Setenv("GOEPAY_MASTER_KEY", "0303030303030303030303030303030303030303030303030303030303030303")
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"78":[7]}`)
}

func TestScanRecovery78WorkerRequiresSeparateExplicitConfiguration(t *testing.T) {
	t.Setenv("GOEPAY_PROVIDER_MODE", "configured")
	t.Setenv("GOEPAY_ENABLE_SCAN_RECOVERY", "")
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", "")
	if f, e := configuredScanRecoveryFlow(nil); e != nil || f != nil {
		t.Fatal("recovery default must not load credentials", e)
	}
	t.Setenv("GOEPAY_ENABLE_SCAN_RECOVERY", "1")
	if _, e := configuredScanRecoveryFlow(nil); e == nil {
		t.Fatal("ambiguous recovery opt-in accepted")
	}
	t.Setenv("GOEPAY_ENABLE_SCAN_RECOVERY", "true")
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "false")
	t.Setenv("GOEPAY_ENABLE_PROVIDER_QUERIES", "true")
	if _, e := configuredScanRecoveryFlow(nil); e == nil {
		t.Fatal("recovery bypassed disabled writes")
	}
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "true")
	if _, e := configuredScanRecoveryFlow(nil); e == nil {
		t.Fatal("recovery discovered implicit credentials")
	}
	recoveryWorkerConfig78(t)
	f, e := configuredScanRecoveryFlow(nil)
	if e != nil || f == nil || !f.ScanRecoveryEnabled || !f.AllowLive || len(f.MerchantChannels[78]) != 1 || !f.Providers[7].(provider.ScanCancellationProvider).CanCancelScan() {
		t.Fatal("explicit recovery flow unavailable", e)
	}
	ro, e := configuredReadOnlyFlow(nil)
	if e != nil || ro == nil || ro.AllowLive || ro.ScanRecoveryEnabled || ro.Providers[7].(provider.ScanCancellationProvider).CanCancelScan() {
		t.Fatal("ordinary reconciliation inherited cancellation capability", e)
	}
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"78":[8]}`)
	if _, e = configuredScanRecoveryFlow(nil); e == nil {
		t.Fatal("recovery accepted unknown admitted channel")
	}
	t.Setenv("GOEPAY_PROVIDER_MODE", "simulation")
	if f, e = configuredScanRecoveryFlow(nil); e != nil || f != nil {
		t.Fatal("simulation reconstructed configured financial recovery", e)
	}
}
