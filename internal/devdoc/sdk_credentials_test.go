package devdoc

import (
	"archive/zip"
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"regexp"
	"strings"
	"testing"
)

func publicSDKEntries(t *testing.T, archive string) map[string][]byte {
	t.Helper()
	raw, err := files.ReadFile("sdk/" + archive)
	if err != nil {
		t.Fatal("public SDK archive unavailable")
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal("public SDK archive invalid")
	}
	entries := make(map[string][]byte)
	for _, entry := range z.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		r, err := entry.Open()
		if err != nil {
			t.Fatal("public SDK entry unavailable")
		}
		body, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal("public SDK entry unreadable")
		}
		entries[entry.Name] = body
	}
	return entries
}

func TestPublicSDKContainsOnlyPortableGoSources(t *testing.T) {
	entries := publicSDKEntries(t, "GoEPay_Go_SDK.zip")
	if string(entries["GoEPay_Go_SDK/go.mod"]) != "module example.com/goepay-sdk\n\ngo 1.22\n" {
		t.Fatal("portable Go module declaration missing")
	}
	for name, body := range entries {
		if strings.HasSuffix(name, ".php") || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".env") || strings.Contains(string(body), "PHP") {
			t.Errorf("public Go archive contains unrelated client or installation material: %s", name)
		}
	}
}

func TestPublicSDKArchivesContainNoPrivateKeyLiteral(t *testing.T) {
	base64Literal := regexp.MustCompile(`['"]([A-Za-z0-9+/=\r\n]{80,})['"]`)
	for _, archive := range []string{"GoEPay_Go_SDK.zip"} {
		t.Run(archive, func(t *testing.T) {
			for name, body := range publicSDKEntries(t, archive) {
				for _, literal := range base64Literal.FindAllSubmatch(body, -1) {
					der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(literal[1])), ""))
					if err != nil {
						continue
					}
					_, pkcs8 := x509.ParsePKCS8PrivateKey(der)
					_, pkcs1 := x509.ParsePKCS1PrivateKey(der)
					_, ec := x509.ParseECPrivateKey(der)
					if pkcs8 == nil || pkcs1 == nil || ec == nil {
						t.Errorf("public SDK entry %s contains a private-key literal", name)
					}
				}
				remaining := body
				for {
					block, rest := pem.Decode(remaining)
					if block == nil {
						break
					}
					if strings.HasSuffix(block.Type, "PRIVATE KEY") {
						t.Errorf("public SDK entry %s contains a PEM private-key literal", name)
					}
					remaining = rest
				}
			}
		})
	}
}
