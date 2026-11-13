package cryptosigner

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// Removing either certificate verification or the authorized SAN check must
// allow this test's unauthenticated/foreign client to obtain a signature.
func TestMTLSSignerRequiresVerifiedAuthorizedClient(t *testing.T) {
	s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	root, serverCert, clientCert, foreignCert := testCertificates(t)
	server := httptest.NewUnstartedServer(NewHandler(s, []string{"payments-api"}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCert}, ClientCAs: root, ClientAuth: tls.RequireAndVerifyClientCert}
	server.StartTLS()
	defer server.Close()
	client, err := NewClient(server.URL, &tls.Config{RootCAs: root, Certificates: []tls.Certificate{clientCert}, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Sign(context.Background(), ethereumPrepared("authorized", 0, "100000"))
	if err != nil || got.TxID == "" {
		t.Fatalf("authorized client: %#v %v", got, err)
	}
	foreign, err := NewClient(server.URL, &tls.Config{RootCAs: root, Certificates: []tls.Certificate{foreignCert}, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = foreign.Sign(context.Background(), ethereumPrepared("foreign", 1, "100000")); err == nil {
		t.Fatal("foreign SAN was authorized")
	}
	unauth := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: root, MinVersion: tls.VersionTLS13}}}
	if resp, err := unauth.Post(server.URL+"/v1/sign", "application/json", nil); err == nil {
		resp.Body.Close()
		t.Fatal("client without certificate reached signer")
	}
}

func TestHandlerRejectsPlainHTTPAndClientRejectsUnverifiedTLS(t *testing.T) {
	s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := httptest.NewRequest(http.MethodPost, "/v1/sign", nil)
	w := httptest.NewRecorder()
	NewHandler(s, []string{"api"}).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("plain HTTP accepted: %d", w.Code)
	}
	if _, err = NewClient("http://localhost:1", &tls.Config{}); err == nil {
		t.Fatal("plaintext client accepted")
	}
	if _, err = NewClient("https://localhost:1", &tls.Config{InsecureSkipVerify: true}); err == nil {
		t.Fatal("unverified client accepted")
	}
}

func testCertificates(t *testing.T) (*x509.CertPool, tls.Certificate, tls.Certificate, tls.Certificate) {
	t.Helper()
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "local signer test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	issue := func(serial int64, name string, server bool) tls.Certificate {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		cert := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		if server {
			cert.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
			cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		}
		b, e := x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, caKey)
		if e != nil {
			t.Fatal(e)
		}
		return tls.Certificate{Certificate: [][]byte{b}, PrivateKey: key}
	}
	return pool, issue(2, "signer", true), issue(3, "payments-api", false), issue(4, "other-service", false)
}
