package payment

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func rejectionClientCertificate76(t *testing.T) tls.Certificate {
	t.Helper()
	key := testKey(t)
	template := &x509.Certificate{SerialNumber: big.NewInt(76), Subject: pkix.Name{CommonName: "synthetic-error-client76"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	cert, e := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if e != nil {
		t.Fatal(e)
	}
	return cert
}

func TestTransferFailure76WechatUnsignedErrorNeedsActualAuthenticatedMTLSAndCorrectEcho(t *testing.T) {
	const apiKey = "synthetic-wechat-error-key76"
	var mode atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, readErr := io.ReadAll(r.Body)
		p, e := decodeXML(raw)
		if readErr != nil || e != nil || !VerifyWechatV2(p, apiKey, "MD5", p["sign"]) {
			t.Error("original wallet request invalid", readErr, e)
		}
		if r.URL.Path != "/mmpaymkttransfers/promotion/transfers" && r.URL.Path != "/mmpaymkttransfers/gettransferinfo" {
			t.Error("wrong operation", r.URL.Path)
		}
		if r.TLS != nil && (len(r.TLS.PeerCertificates) != 1 || r.TLS.PeerCertificates[0].Subject.CommonName != "synthetic-error-client76") {
			t.Error("actual client certificate missing")
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "FAIL", "err_code": "NOTENOUGH"}
		switch mode.Load() {
		case 1:
			out["mchid"] = "wrong-mch"
		case 2:
			out["return_code"] = "FAIL"
		case 3:
			w.WriteHeader(503)
			return
		}
		w.Write(encodeXML(out))
	})
	up := httptest.NewUnstartedServer(handler)
	up.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert}
	up.StartTLS()
	defer up.Close()
	transport := up.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.Certificates = []tls.Certificate{rejectionClientCertificate76(t)}
	client := &http.Client{Transport: transport}
	defer transport.CloseIdleConnections()
	cfg := WechatV2Config{AppID: "wx76", MerchantID: "mch76", APIKey: apiKey, BaseURL: up.URL, HTTPClient: client, RefundClient: client}
	p, e := NewWechatV2(cfg)
	if e != nil {
		t.Fatal(e)
	}
	in := TransferRequest{TransferNo: "TF76", Type: "wxpay", Account: "synthetic-openid", Amount: 450, Remark: "local only"}
	_, sendErr := p.Transfer(context.Background(), in)
	v, ok := VerifiedTransferFailure(sendErr)
	scope, _ := TransferAccountScope(p)
	if !ok || v.Provider != "wxpay" || v.Product != p.TransferProductIdentity() || v.Operation != "/mmpaymkttransfers/promotion/transfers" || v.TransferNo != "TF76" || v.Amount != 450 || v.AccountScope != scope || v.Code != "NOTENOUGH" {
		t.Fatal("actual mTLS failure lost original source", v, ok, sendErr)
	}
	for _, wrapped := range []error{&UnknownOutcome{Operation: "transfer", Cause: sendErr}, errors.Join(sendErr, context.Canceled), errors.Join(sendErr, context.DeadlineExceeded)} {
		if _, valid := VerifiedTransferFailure(p.bindTransferFailure(wrapped, in, v.Operation, v.Product)); valid {
			t.Fatal("binding stripped original transfer uncertainty")
		}
	}
	changed := in
	changed.TransferNo = "TF77"
	if _, valid := VerifiedTransferFailure(p.bindTransferFailure(sendErr, changed, v.Operation, v.Product)); valid {
		t.Fatal("original submit failure rebound to another transfer")
	}
	_, queryErr := p.QueryTransfer(context.Background(), in)
	if _, ok = VerifiedTransferFailure(queryErr); ok {
		t.Fatal("read-only query failure became submit evidence")
	}
	for _, bad := range []int32{1, 2, 3} {
		mode.Store(bad)
		_, e = p.Transfer(context.Background(), in)
		if _, ok = VerifiedTransferFailure(e); ok {
			t.Fatal("untrusted/inconsistent response became stop evidence", bad, e)
		}
	}
	mode.Store(0)
	plain := httptest.NewServer(handler)
	defer plain.Close()
	cfg.BaseURL = plain.URL
	cfg.HTTPClient = plain.Client()
	cfg.RefundClient = plain.Client()
	weak, e := NewWechatV2(cfg)
	if e != nil {
		t.Fatal(e)
	}
	_, e = weak.Transfer(context.Background(), in)
	if _, ok = VerifiedTransferFailure(e); ok {
		t.Fatal("unsigned plain HTTP acquired authenticated source", e)
	}
}
