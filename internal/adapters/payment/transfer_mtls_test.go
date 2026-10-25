package payment

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWechatTransferRequiresActualClientCertificate(t *testing.T) {
	key := testKey(t)
	template := &x509.Certificate{SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: "synthetic-transfer-client"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	cert, e := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if e != nil {
		t.Fatal(e)
	}
	requests := 0
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if len(r.TLS.PeerCertificates) != 1 || r.TLS.PeerCertificates[0].Subject.CommonName != "synthetic-transfer-client" {
			t.Error("client certificate missing")
		}
		w.Write(encodeXML(map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "mch_appid": "app", "mchid": "mch", "partner_trade_no": "TF42", "payment_no": "wx42", "payment_time": "2026-10-23 12:00:00"}))
	}))
	upstream.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert}
	upstream.StartTLS()
	defer upstream.Close()
	client := &http.Client{Transport: upstream.Client().Transport}
	transport := client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
	client.Transport = transport
	req := TransferRequest{TransferNo: "TF42", Type: "wxpay", Account: "synthetic-openid", Amount: 100, Remark: "test"}
	provider, e := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: "synthetic-api-key-long-enough", BaseURL: upstream.URL, HTTPClient: upstream.Client(), RefundClient: client})
	if e != nil {
		t.Fatal(e)
	}
	if result, e := provider.Transfer(context.Background(), req); e != nil || result.State != "SUCCESS" {
		t.Fatal(result, e)
	}
	provider, e = NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: "synthetic-api-key-long-enough", BaseURL: upstream.URL, HTTPClient: upstream.Client(), RefundClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	var unknown *UnknownOutcome
	if _, e = provider.Transfer(context.Background(), req); !errors.As(e, &unknown) {
		t.Fatal("failed mTLS write did not remain unknown", e)
	}
	if requests != 1 {
		t.Fatal("unauthenticated request reached upstream", requests)
	}
}
