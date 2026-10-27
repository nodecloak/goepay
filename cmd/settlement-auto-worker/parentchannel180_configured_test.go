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
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"golang.org/x/crypto/hkdf"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestParentChannel180AutoWorkerInstallsSameVersionRestorerAndRootWriteGate(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	private := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	publicBytes, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicBytes}))
	imp := channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "alipay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "synthetic-auto180", "appmchid": "2088000000012345", "appsecret": private, "appkey": public}}}}
	master := bytes.Repeat([]byte{18}, 32)
	plain, _ := json.Marshal(imp)
	derived := make([]byte, 32)
	io.ReadFull(hkdf.New(sha256.New, master, nil, []byte("goepay:imported-channels:v1")), derived)
	block, _ := aes.NewCipher(derived)
	aead, _ := cipher.NewGCM(block)
	nonce := bytes.Repeat([]byte{19}, 12)
	sealed := aead.Seal(nil, nonce, plain, []byte("goepay:imported-channels:v1"))
	raw, _ := json.Marshal(map[string]any{"version": 1, "aad": "goepay:imported-channels:v1", "nonce": base64.StdEncoding.EncodeToString(nonce), "ciphertext": base64.StdEncoding.EncodeToString(sealed[:len(sealed)-16]), "tag": base64.StdEncoding.EncodeToString(sealed[len(sealed)-16:])})
	path := filepath.Join(t.TempDir(), "synthetic180.enc.json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", path)
	t.Setenv("GOEPAY_MASTER_KEY", "1212121212121212121212121212121212121212121212121212121212121212")
	t.Setenv("GOEPAY_PROVIDER_MODE", "configured")
	t.Setenv("GOEPAY_SANDBOX", "false")
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "true")
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1":[7]}`)
	t.Setenv("GOEPAY_ENABLE_CHANNEL_AUTO_STOP", "false")
	flow, e := configuredFlow()
	if e != nil {
		t.Fatal(e)
	}
	factory := flow.ParentFactories[7]
	if factory == nil {
		t.Fatal("automatic worker has no immutable parent account restorer")
	}
	execution, _, e := factory.BootstrapSnapshot(7)
	if e != nil {
		t.Fatal(e)
	}
	v, ok := execution.Payment.(adapter.CreateValidator)
	if !ok {
		t.Fatal("real constructor validator missing")
	}
	flow.AllowLive = false
	if e = v.ValidateCreate(adapter.CreateRequest{TradeNo: "synthetic180", Subject: "pure test", Method: execution.Payment.Capabilities().Methods[0], NotifyURL: "https://pay.example.invalid/callback", Amount: 100}); !errors.Is(e, channelconfig.ErrLiveDisabled) {
		t.Fatal("prepared worker adapter bypassed current root gate", e)
	}
}
