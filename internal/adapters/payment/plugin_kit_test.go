package payment

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func pluginKitTestPEM(kind string, der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}))
}

// Acquirers still hand out 1024-bit RSA keys and .cer certificates; the kit
// must read every form the original plugins read with openssl.
func TestPluginKitAcquirerKeyForms(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(key)
	pkix8, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "acquirer"}, NotBefore: time.Unix(0, 0), NotAfter: time.Unix(4102444800, 0)}
	cert, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"pkcs1 pem":   pluginKitTestPEM("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key)),
		"pkcs8 pem":   pluginKitTestPEM("PRIVATE KEY", pkcs8),
		"pkcs8 bare":  base64.StdEncoding.EncodeToString(pkcs8),
		"pkcs1 lines": "\n" + base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key))[:40] + "\n" + base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key))[40:] + "\n",
	} {
		k, e := pluginPrivateKey(text)
		if e != nil || k.N.Cmp(key.N) != 0 {
			t.Fatalf("private %s: %v", name, e)
		}
	}
	for name, text := range map[string]string{
		"pkix pem":  pluginKitTestPEM("PUBLIC KEY", pkix8),
		"pkix bare": base64.StdEncoding.EncodeToString(pkix8),
		"pkcs1 pem": pluginKitTestPEM("RSA PUBLIC KEY", x509.MarshalPKCS1PublicKey(&key.PublicKey)),
		"cert pem":  pluginKitTestPEM("CERTIFICATE", cert),
		"cert bare": base64.StdEncoding.EncodeToString(cert),
	} {
		k, e := pluginPublicKey(text)
		if e != nil || k.N.Cmp(key.N) != 0 {
			t.Fatalf("public %s: %v", name, e)
		}
	}
	// The parsed pair still signs and verifies (SHA1 is what 1024-bit acquirers use).
	priv, _ := pluginPrivateKey(base64.StdEncoding.EncodeToString(pkcs8))
	pub, _ := pluginPublicKey(pluginKitTestPEM("CERTIFICATE", cert))
	sig, err := rsaSign(priv, crypto.SHA1, "a=1&b=2")
	if err != nil || !rsaVerify(pub, crypto.SHA1, "a=1&b=2", sig) {
		t.Fatalf("sign/verify with parsed keys: %v", err)
	}
}

func TestPluginKitRefusesOtherKeys(t *testing.T) {
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ecPriv, _ := x509.MarshalPKCS8PrivateKey(ec)
	ecPub, _ := x509.MarshalPKIXPublicKey(&ec.PublicKey)
	if _, e := pluginPrivateKey(pluginKitTestPEM("PRIVATE KEY", ecPriv)); e == nil {
		t.Fatal("EC private key accepted as RSA")
	}
	if _, e := pluginPublicKey(pluginKitTestPEM("PUBLIC KEY", ecPub)); e == nil {
		t.Fatal("EC public key accepted as RSA")
	}
	key, _ := rsa.GenerateKey(rand.Reader, 1024)
	twoBlocks := pluginKitTestPEM("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key)) + pluginKitTestPEM("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key))
	for name, text := range map[string]string{"empty": "", "garbage": "not a key", "two pem blocks": twoBlocks, "path": "/etc/ssl/key.pem"} {
		if _, e := pluginPrivateKey(text); e == nil {
			t.Fatalf("private %s accepted", name)
		}
		if _, e := pluginPublicKey(text); e == nil {
			t.Fatalf("public %s accepted", name)
		}
	}
}
