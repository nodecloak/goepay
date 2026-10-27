package channelconfig

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	provider "goepay/internal/adapters/payment"
	"math/big"
	"testing"
	"time"
)

func certificateChannel(t *testing.T) Channel {
	return certificateChannelAt(t, time.Now())
}
func certificateChannelAt(t *testing.T, now time.Time) Channel {
	t.Helper()
	rootKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	appKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	platformKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{Country: []string{"CN"}, Organization: []string{"Synthetic CA"}, CommonName: "Synthetic Root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, e := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	if e != nil {
		t.Fatal(e)
	}
	root, e = x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	certs := map[string]string{"root_cert_path": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
	for i, item := range []struct {
		name string
		key  *rsa.PrivateKey
	}{{"app_cert_path", appKey}, {"alipay_cert_path", platformKey}} {
		leaf := &x509.Certificate{SerialNumber: big.NewInt(int64(i + 2)), Subject: pkix.Name{CommonName: item.name}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(30 * time.Minute), KeyUsage: x509.KeyUsageDigitalSignature}
		der, e = x509.CreateCertificate(rand.Reader, leaf, root, &item.key.PublicKey, rootKey)
		if e != nil {
			t.Fatal(e)
		}
		certs[item.name] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	}
	return Channel{ID: 9, Plugin: "alipay", AppTypes: []string{"1", "6", "10"}, Config: map[string]string{"appid": "synthetic-app", "sign_type": "1", "appsecret": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(appKey)}))}, Certificates: certs}
}

func TestAlipayCertificateFactoryExplicitOfflineAndWriteGuard(t *testing.T) {
	c := certificateChannel(t)
	clients, e := Build(Import{Version: 1, Channels: []Channel{c}}, false)
	if e != nil {
		t.Fatal("complete certificate mode rejected", e)
	}
	if _, e = clients[9].Create(context.Background(), provider.CreateRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("certificate write unguarded", e)
	}
	if _, e = clients[9].(provider.TransferProvider).Transfer(context.Background(), provider.TransferRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("certificate transfer unguarded", e)
	}
	for _, missing := range []string{"app_cert_path", "alipay_cert_path", "root_cert_path"} {
		original := c.Certificates[missing]
		delete(c.Certificates, missing)
		if _, e = Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
			t.Fatal("incomplete certificate mode accepted", missing)
		}
		c.Certificates[missing] = original
	}
	c.Config["sign_type"] = "0"
	if _, e = Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
		t.Fatal("certificates silently activated in public-key mode")
	}
}
