package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"goepay/internal/channelconfig"
	"goepay/internal/server"
	"golang.org/x/crypto/hkdf"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func oauthImportFile(t *testing.T, master []byte, imp channelconfig.Import) string {
	t.Helper()
	plain, e := json.Marshal(imp)
	if e != nil {
		t.Fatal(e)
	}
	key := make([]byte, 32)
	if _, e = io.ReadFull(hkdf.New(sha256.New, master, nil, []byte("goepay:imported-channels:v1")), key); e != nil {
		t.Fatal(e)
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		t.Fatal(e)
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		t.Fatal(e)
	}
	nonce := make([]byte, g.NonceSize())
	rand.Read(nonce)
	sealed := g.Seal(nil, nonce, plain, []byte("goepay:imported-channels:v1"))
	raw, _ := json.Marshal(map[string]any{"version": 1, "aad": "goepay:imported-channels:v1", "nonce": base64.StdEncoding.EncodeToString(nonce), "ciphertext": base64.StdEncoding.EncodeToString(sealed[:len(sealed)-16]), "tag": base64.StdEncoding.EncodeToString(sealed[len(sealed)-16:])})
	path := filepath.Join(t.TempDir(), "synthetic-oauth.enc.json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestConfiguredOAuthRequiresBothEncryptedChannelAndExplicitGlobalOptIn(t *testing.T) {
	master := bytes.Repeat([]byte{3}, 32)
	c := channelconfig.Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"2"}, Config: map[string]string{"appid": "wxapp", "appmchid": "mch", "appkey": "0123456789abcdef0123456789abcdef", "oauth_appsecret": "test-only-oauth-secret", "enable_web_oauth": "true"}}
	path := oauthImportFile(t, master, channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{c}})
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", path)
	t.Setenv("GOEPAY_MERCHANT_CHANNELS", `{"1000":[7]}`)
	t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "false")
	t.Setenv("GOEPAY_ENABLE_OAUTH", "")
	app := &server.Server{}
	if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e != nil || len(app.OAuthProviders) != 0 {
		t.Fatal("channel setting silently enabled OAuth", e)
	}
	t.Setenv("GOEPAY_ENABLE_OAUTH", "true")
	if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e != nil || app.OAuthProviders[7].ApplicationID() != "wxapp" || app.ProviderFlow.AllowLive {
		t.Fatal("explicit offline OAuth configuration invalid", e)
	}
	c.Config["enable_web_oauth"] = "false"
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", oauthImportFile(t, master, channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{c}}))
	if e := configureProviders(&server.Server{}, "configured", master, "https://pay.example.invalid"); e == nil {
		t.Fatal("global OAuth opt-in bypassed channel opt-in")
	}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", path)
	t.Setenv("GOEPAY_ENABLE_OAUTH", "false")
	if e := configureProviders(app, "configured", master, "https://pay.example.invalid"); e != nil || len(app.OAuthProviders) != 0 {
		t.Fatal("disabled OAuth retained active providers", e)
	}
	t.Setenv("GOEPAY_ENABLE_OAUTH", "yes")
	if e := configureProviders(&server.Server{}, "configured", master, "https://pay.example.invalid"); e == nil {
		t.Fatal("ambiguous OAuth opt-in accepted")
	}
	t.Setenv("GOEPAY_ENABLE_OAUTH", "true")
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", "")
	if e := configureProviders(&server.Server{}, "configured", master, "https://pay.example.invalid"); e == nil {
		t.Fatal("OAuth discovered credentials without an explicit encrypted path")
	}
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", "must-not-read-in-simulation")
	simulation := &server.Server{Sandbox: true}
	if e := configureProviders(simulation, "simulation", master, "http://127.0.0.1:8180"); e != nil || len(simulation.OAuthProviders) != 0 {
		t.Fatal("simulation loaded OAuth credentials", e)
	}
}
