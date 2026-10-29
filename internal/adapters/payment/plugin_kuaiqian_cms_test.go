package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// kuaiqianTestCert makes a self-signed RSA cert+key for CMS tests.
func kuaiqianTestCert(t *testing.T, serial int64) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "kq-test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, e := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	return key, cert
}

// kuaiqianCMSPlugin builds a plugin whose merchant identity is (mKey, mCert)
// and whose peer (platform/kuaiqian) is pCert. Sealing envelopes to pCert and
// signs with mKey; unsealing decrypts with mKey.
func kuaiqianCMSPlugin(t *testing.T, mKey *rsa.PrivateKey, mCert, pCert *x509.Certificate) *kuaiqianPlugin {
	t.Helper()
	base, e := newPluginBase(PluginChannel{Plugin: "kuaiqian", PayType: "alipay"}, pluginAccount("https://www.99bill.com", "9999"))
	if e != nil {
		t.Fatal(e)
	}
	return &kuaiqianPlugin{pluginBase: base, appid: "9999", privateKey: mKey, publicKey: &mKey.PublicKey, merchantCert: mCert, platformCert: pCert, gateway: kuaiqianUmgwGateway}
}

// TestPluginKuaiqianCMSRoundTrip seals with the merchant signing + platform
// recipient, then opens with the platform key and verifies the detached
// signature against the embedded merchant cert — independent key directions.
func TestPluginKuaiqianCMSRoundTrip(t *testing.T) {
	mKey, mCert := kuaiqianTestCert(t, 111)
	pKey, pCert := kuaiqianTestCert(t, 222)
	p := kuaiqianCMSPlugin(t, mKey, mCert, pCert)

	plain := []byte(`{"orderId":"T1","amount":"100"}`)
	signedDER, envelopedDER, e := p.kuaiqianSeal(plain)
	if e != nil {
		t.Fatal(e)
	}
	// Independent open: decrypt the envelope with the platform key (it was the
	// recipient), then verify the detached signature over the plaintext.
	got, e := kuaiqianOpenEnvelopedData(envelopedDER, pKey)
	if e != nil || string(got) != string(plain) {
		t.Fatal("decrypt mismatch", string(got), e)
	}
	if !kuaiqianVerifySignedData(signedDER, plain) {
		t.Fatal("detached signature did not verify")
	}
	// Tampered plaintext must fail verification.
	if kuaiqianVerifySignedData(signedDER, []byte(`{"orderId":"T1","amount":"999"}`)) {
		t.Fatal("verify accepted tampered content")
	}
	// Wrong recipient key must fail to decrypt.
	if _, e := kuaiqianOpenEnvelopedData(envelopedDER, mKey); e == nil {
		t.Fatal("decrypt accepted wrong key")
	}
}

// TestPluginKuaiqianCMSStructure assembles/inspects the ContentInfo field by
// field to confirm the OID wrapping and SignedData shape.
func TestPluginKuaiqianCMSStructure(t *testing.T) {
	mKey, mCert := kuaiqianTestCert(t, 7)
	_, pCert := kuaiqianTestCert(t, 8)
	p := kuaiqianCMSPlugin(t, mKey, mCert, pCert)
	signedDER, envelopedDER, e := p.kuaiqianSeal([]byte("hello"))
	if e != nil {
		t.Fatal(e)
	}
	// SignedData ContentInfo carries the signedData OID; its inner SignedData
	// has a detached EncapContentInfo (no eContent) and one signer.
	oid, inner, e := kuaiqianUnwrapContentInfo(signedDER)
	if e != nil || !oid.Equal(kuaiqianOIDSignedData) {
		t.Fatal("signed OID", oid, e)
	}
	var sd kuaiqianSignedData
	if _, e := asn1.Unmarshal(inner, &sd); e != nil {
		t.Fatal(e)
	}
	if len(sd.SignerInfos) != 1 || !sd.EncapContentInfo.EContentType.Equal(kuaiqianOIDData) || len(sd.EncapContentInfo.EContent.Bytes) != 0 {
		t.Fatal("unexpected SignedData shape", sd)
	}
	if !sd.SignerInfos[0].DigestAlgorithm.Algorithm.Equal(kuaiqianOIDSHA256) {
		t.Fatal("digest alg")
	}
	// EnvelopedData ContentInfo carries the envelopedData OID with AES-128-CBC.
	oid, inner, e = kuaiqianUnwrapContentInfo(envelopedDER)
	if e != nil || !oid.Equal(kuaiqianOIDEnveloped) {
		t.Fatal("enveloped OID", oid, e)
	}
	var ed kuaiqianEnvelopedData
	if _, e := asn1.Unmarshal(inner, &ed); e != nil {
		t.Fatal(e)
	}
	if len(ed.RecipientInfos) != 1 || !ed.RecipientInfos[0].KeyEncryptionAlgorithm.Algorithm.Equal(kuaiqianOIDRSA) || !ed.EncryptedContentInfo.ContentEncryptionAlgorithm.Algorithm.Equal(kuaiqianOIDAES128CBC) {
		t.Fatal("unexpected EnvelopedData shape", ed)
	}
}

// kuaiqianServer acts as the 99bill umgw endpoint for Query/Refund: it decrypts
// the request (enveloped to the platform cert) with the platform key, verifies
// the merchant's detached signature, then returns a signed+enveloped (to the
// merchant cert) response sealed with the platform key.
func kuaiqianServer(t *testing.T, pKey *rsa.PrivateKey, pCert, mCert *x509.Certificate, respObj map[string]string, responseCode string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req map[string]any
		if e := json.Unmarshal(b, &req); e != nil {
			w.WriteHeader(400)
			return
		}
		reqBody, _ := req["requestBody"].(map[string]any)
		signed, _ := base64.StdEncoding.DecodeString(jsonText(reqBody, "signedData"))
		enveloped, _ := base64.StdEncoding.DecodeString(jsonText(reqBody, "envelopedData"))
		plain, e := kuaiqianOpenEnvelopedData(enveloped, pKey)
		if e != nil {
			w.WriteHeader(400)
			io.WriteString(w, "decrypt failed")
			return
		}
		if !kuaiqianVerifySignedData(signed, plain) {
			w.WriteHeader(400)
			io.WriteString(w, "verify failed")
			return
		}
		if responseCode != "0000" {
			out, _ := json.Marshal(map[string]any{"head": map[string]string{"responseCode": responseCode, "responseTextMessage": "x"}})
			w.Write(out)
			return
		}
		respRaw, _ := json.Marshal(respObj)
		rSigned, e1 := kuaiqianBuildSignedData(respRaw, pCert, pKey)
		rEnv, e2 := kuaiqianBuildEnvelopedData(respRaw, mCert)
		if e1 != nil || e2 != nil {
			w.WriteHeader(500)
			return
		}
		out, _ := json.Marshal(map[string]any{
			"head": map[string]string{"responseCode": "0000"},
			"responseBody": map[string]string{
				"signedData":    base64.StdEncoding.EncodeToString(rSigned),
				"envelopedData": base64.StdEncoding.EncodeToString(rEnv),
			},
		})
		w.Write(out)
	}))
}

func TestPluginKuaiqianQuery(t *testing.T) {
	mKey, mCert := kuaiqianTestCert(t, 111)
	pKey, pCert := kuaiqianTestCert(t, 222)
	p := kuaiqianCMSPlugin(t, mKey, mCert, pCert)

	srv := kuaiqianServer(t, pKey, pCert, mCert, map[string]string{"orderId": "T1", "payResult": "10", "orderAmount": "100", "dealId": "D9"}, "0000")
	defer srv.Close()
	p.gateway = srv.URL

	if !p.Capabilities().Query || !p.Capabilities().Refund {
		t.Fatal("capabilities not advertised")
	}
	q, e := p.Query(context.Background(), QueryRequest{TradeNo: "T1"})
	if e != nil || q.State != "SUCCESS" || q.Amount != 100 || q.ProviderTradeNo != "D9" {
		t.Fatal(q, e)
	}

	// Foreign order id in the (verified) response is rejected.
	srv2 := kuaiqianServer(t, pKey, pCert, mCert, map[string]string{"orderId": "OTHER", "payResult": "10", "orderAmount": "100"}, "0000")
	defer srv2.Close()
	p.gateway = srv2.URL
	if _, e = p.Query(context.Background(), QueryRequest{TradeNo: "T1"}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("foreign order", e)
	}

	// Business refusal (responseCode != 0000) is definitive.
	srv3 := kuaiqianServer(t, pKey, pCert, mCert, nil, "W101")
	defer srv3.Close()
	p.gateway = srv3.URL
	var pe *ProviderError
	if _, e = p.Query(context.Background(), QueryRequest{TradeNo: "T1"}); !errors.As(e, &pe) {
		t.Fatal("business refusal", e)
	}
}

func TestPluginKuaiqianRefund(t *testing.T) {
	mKey, mCert := kuaiqianTestCert(t, 111)
	pKey, pCert := kuaiqianTestCert(t, 222)
	p := kuaiqianCMSPlugin(t, mKey, mCert, pCert)

	srv := kuaiqianServer(t, pKey, pCert, mCert, map[string]string{"bizResponseCode": "0000"}, "0000")
	defer srv.Close()
	p.gateway = srv.URL
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "20261029000001", RefundNo: "RF1", Amount: 500, TotalAmount: 1000})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 500 {
		t.Fatal(rr, e)
	}

	// Refund business refusal: responseCode 0000 but bizResponseCode != 0000.
	srv2 := kuaiqianServer(t, pKey, pCert, mCert, map[string]string{"bizResponseCode": "F999"}, "0000")
	defer srv2.Close()
	p.gateway = srv2.URL
	var pe *ProviderError
	if _, e = p.Refund(context.Background(), RefundRequest{TradeNo: "20261029000001", RefundNo: "RF2", Amount: 500, TotalAmount: 1000}); !errors.As(e, &pe) {
		t.Fatal("biz refusal", e)
	}

	// Local validation.
	if _, e = p.Refund(context.Background(), RefundRequest{TradeNo: "T", RefundNo: "", Amount: 1, TotalAmount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("needs refund no", e)
	}

	// Build without certs (bare public key) rejects CMS with ErrUnsupported.
	base, _ := newPluginBase(PluginChannel{Plugin: "kuaiqian", PayType: "alipay"}, pluginAccount("https://www.99bill.com", "9999"))
	noCerts := &kuaiqianPlugin{pluginBase: base, appid: "9999", privateKey: mKey, publicKey: &mKey.PublicKey, gateway: kuaiqianUmgwGateway}
	if _, e = noCerts.Refund(context.Background(), RefundRequest{TradeNo: "20261029000001", RefundNo: "RF3", Amount: 500, TotalAmount: 1000}); !errors.Is(e, ErrUnsupported) {
		t.Fatal("no certs should be unsupported", e)
	}
}
