package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goepay/internal/channelconfig"
	"goepay/internal/compat"
)

func testAlipayKeys(t *testing.T) (private, public string) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pub, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub}))
}

func TestSealChannelsProducesStartupReadableImport(t *testing.T) {
	master := bytes.Repeat([]byte{7}, 32)
	private, public := testAlipayKeys(t)
	plain := `{"version":1,"channels":[{"id":3,"plugin":"alipay","app_types":["1","2"],"config":{"appid":"2021000000000000","appsecret":` + jsonString(private) + `,"appkey":` + jsonString(public) + `},"certificates":{}}]}`
	var out, report bytes.Buffer
	if e := sealChannels(strings.NewReader(plain), &out, &report, master); e != nil {
		t.Fatal(e)
	}
	imp, e := channelconfig.Decrypt(bytes.TrimSpace(out.Bytes()), master)
	if e != nil || len(imp.Channels) != 1 || imp.Channels[0].ID != 3 {
		t.Fatalf("sealed import is not readable by startup: %v", e)
	}
	if strings.Contains(report.String(), "BEGIN") || strings.Contains(out.String(), "2021000000000000") {
		t.Fatal("secrets or plaintext leaked into the output")
	}
	if !strings.Contains(report.String(), "sealed channel 3 plugin=alipay app_types=1,2") {
		t.Fatalf("missing summary: %q", report.String())
	}
}

func TestSealChannelsRejectsAmbiguousOrIncompleteInput(t *testing.T) {
	master := bytes.Repeat([]byte{7}, 32)
	for name, plain := range map[string]string{
		"misspelled field": `{"version":1,"channels":[{"id":3,"plugin":"alipay","app_type":["1"],"config":{},"certificates":{}}]}`,
		"no channels":      `{"version":1,"channels":[]}`,
		"wrong version":    `{"version":2,"channels":[]}`,
		"incomplete":       `{"version":1,"channels":[{"id":3,"plugin":"alipay","app_types":["1"],"config":{"appid":"x"},"certificates":{}}]}`,
		"trailing data":    `{"version":1,"channels":[]} {}`,
	} {
		var out, report bytes.Buffer
		if e := sealChannels(strings.NewReader(plain), &out, &report, master); e == nil || out.Len() != 0 {
			t.Fatalf("%s: accepted (%v)", name, e)
		}
	}
}

func TestGeneratePlatformKeyNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.pem")
	var public bytes.Buffer
	if e := generatePlatformKey(path, &public); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private key file must be owner-only: %v %v", e, info.Mode())
	}
	written, _ := os.ReadFile(path)
	key, e := compat.ParsePrivateKey(string(written))
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := compat.ParsePublicKey(public.String())
	if e != nil || !parsed.Equal(&key.PublicKey) {
		t.Fatal("printed public key does not match the stored private key")
	}
	if e = generatePlatformKey(path, &public); e == nil {
		t.Fatal("existing platform key overwritten")
	}
	again, _ := os.ReadFile(path)
	if !bytes.Equal(again, written) {
		t.Fatal("existing platform key changed")
	}
	if e = generatePlatformKey("", &public); e == nil {
		t.Fatal("missing path accepted")
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
