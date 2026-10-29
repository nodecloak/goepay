package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// alipaygTestKeys returns a merchant private key PEM and the Antom public key
// PEM for a freshly generated 2048-bit pair, plus the parsed keys.
func alipaygTestKeys(t *testing.T) (privPEM, pubPEM string, priv *rsa.PrivateKey) {
	t.Helper()
	k, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	privPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustPKCS8(t, k)}))
	pubDER, e := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	pubPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	return privPEM, pubPEM, k
}

func mustPKCS8(t *testing.T, k *rsa.PrivateKey) []byte {
	t.Helper()
	der, e := x509.MarshalPKCS8PrivateKey(k)
	if e != nil {
		t.Fatal(e)
	}
	return der
}

func alipaygChannel(t *testing.T, currency, rate string) (PluginChannel, *rsa.PrivateKey) {
	privPEM, pubPEM, priv := alipaygTestKeys(t)
	return PluginChannel{ID: 9, Plugin: "alipayg", PayType: "alipay",
		Config:       map[string]string{"appid": "CLIENTID1", "appswitch": "0", "currency_code": currency, "currency_rate": rate},
		Certificates: map[string]string{"appsecret": privPEM, "appkey": pubPEM},
	}, priv
}

func TestPluginAlipaygRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("alipayg")
	if !ok || spec.Title != "支付宝国际版" || !spec.Serves("alipay") || spec.Serves("wxpay") {
		t.Fatal("spec", spec, ok)
	}
	c, _ := alipaygChannel(t, "CNY", "1")
	if _, e := BuildPluginChannel(c); e != nil {
		t.Fatal(e)
	}
	// Missing private key.
	missing, _ := alipaygChannel(t, "CNY", "1")
	delete(missing.Certificates, "appsecret")
	// Unserved pay type.
	unserved, _ := alipaygChannel(t, "CNY", "1")
	unserved.PayType = "wxpay"
	// Bad gateway option.
	badgw, _ := alipaygChannel(t, "CNY", "1")
	badgw.Config["appswitch"] = "7"
	for i, bad := range []PluginChannel{missing, unserved, badgw} {
		if _, e := BuildPluginChannel(bad); e == nil {
			t.Fatal("accepted bad", i)
		}
	}
}

func alipaygTestPlugin(t *testing.T, host, currency, rate string) (*alipaygPlugin, *rsa.PrivateKey) {
	c, priv := alipaygChannel(t, currency, rate)
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	pp := p.(*alipaygPlugin)
	pp.gateway = host
	return pp, priv
}

func TestPluginAlipaygCreateJump(t *testing.T) {
	var gotPath string
	var gotSig, gotTime, gotClient string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSig = r.Header.Get("Signature")
		gotTime = r.Header.Get("Request-Time")
		gotClient = r.Header.Get("client-id")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		io.WriteString(w, `{"result":{"resultStatus":"S","resultCode":"SUCCESS"},"paymentId":"PAY1","normalUrl":"https://global.alipay.com/pay/PAY1"}`)
	}))
	defer srv.Close()
	p, _ := alipaygTestPlugin(t, srv.URL, "CNY", "1")
	r := CreateRequest{TradeNo: "T1", Subject: "Goods", Method: "page", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 1000}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "jump" || res.Material != "https://global.alipay.com/pay/PAY1" || res.ProviderTradeNo != "PAY1" {
		t.Fatal(res, e)
	}
	if gotPath != "/ams/api/v1/payments/pay" || gotClient != "CLIENTID1" || gotTime == "" {
		t.Fatal("request shape", gotPath, gotClient, gotTime)
	}
	if !alipaygHeaderWellFormed(gotSig) {
		t.Fatal("signature header", gotSig)
	}
	// amount.total is 1000 cents -> value "1000" (CNY minor units).
	pay := body["paymentAmount"].(map[string]any)
	if pay["value"] != "1000" || pay["currency"] != "CNY" {
		t.Fatal("amount", pay)
	}
	// Business refusal is definitive.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"result":{"resultStatus":"F","resultCode":"ORDER_IS_CLOSED","resultMessage":"closed"}}`)
	}))
	defer srv2.Close()
	p.gateway = srv2.URL
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("refusal must be definitive", e)
	}
}

func alipaygHeaderWellFormed(h string) bool {
	return len(h) > 0 && alipaygSignatureValue(h) != ""
}

func TestPluginAlipaygVerifyNotification(t *testing.T) {
	p, priv := alipaygTestPlugin(t, "https://unused", "CNY", "1")
	// Body as Antom posts it.
	body := `{"result":{"resultStatus":"S"},"paymentRequestId":"T202610290001","paymentId":"PAYX","paymentAmount":{"currency":"CNY","value":"100"},"pspCustomerInfo":{"pspCustomerId":"2088buyer"}}`
	// The signed request-target is the fixed callback path for channel 9.
	path := "/callbacks/providers/9/" + url.PathEscape("T202610290001")
	respTime := "1730000000000"
	content := "POST " + path + "\n" + "CLIENTID1." + respTime + "." + body
	sig := alipaygSignWith(t, priv, content)
	hdr := http.Header{}
	hdr.Set("Request-Time", respTime)
	hdr.Set("Signature", "algorithm=RSA256,keyVersion=1,signature="+url.QueryEscape(sig))
	exp := ExpectedPayment{TradeNo: "T202610290001", Amount: 100}
	ev, e := p.VerifyNotification(context.Background(), hdr, []byte(body), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "PAYX" || ev.BuyerID != "2088buyer" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// Bad signature.
	bad := http.Header{}
	bad.Set("Request-Time", respTime)
	bad.Set("Signature", "algorithm=RSA256,signature="+url.QueryEscape(alipaygSignWith(t, priv, "POST /wrong\n"+content)))
	if _, e := p.VerifyNotification(context.Background(), bad, []byte(body), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// Wrong amount / order (correctly signed otherwise).
	if _, e := p.VerifyNotification(context.Background(), hdr, []byte(body), ExpectedPayment{TradeNo: "T202610290001", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e := p.VerifyNotification(context.Background(), hdr, []byte(body), ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("order", e)
	}
	// Non-success status.
	body2 := `{"result":{"resultStatus":"F"},"paymentRequestId":"T202610290001","paymentAmount":{"currency":"CNY","value":"100"}}`
	content2 := "POST " + path + "\n" + "CLIENTID1." + respTime + "." + body2
	hdr2 := http.Header{}
	hdr2.Set("Request-Time", respTime)
	hdr2.Set("Signature", "signature="+url.QueryEscape(alipaygSignWith(t, priv, content2)))
	if _, e := p.VerifyNotification(context.Background(), hdr2, []byte(body2), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success", e)
	}
	// Ack bodies.
	if s, ct, b := p.NotificationAck(nil); s != 200 || ct == "" || b == "" {
		t.Fatal(s, ct, b)
	}
}

func TestPluginAlipaygRefundAndClose(t *testing.T) {
	var lastPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		if lastPath == "/ams/api/v1/payments/refund" {
			io.WriteString(w, `{"result":{"resultStatus":"S"},"refundId":"RF1","refundAmount":{"value":"500"}}`)
		} else {
			io.WriteString(w, `{"result":{"resultStatus":"S"}}`)
		}
	}))
	defer srv.Close()
	p, _ := alipaygTestPlugin(t, srv.URL, "CNY", "1")
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "PAYX", RefundNo: "R1", Amount: 500, TotalAmount: 1000})
	if e != nil || rr.State != "SUCCESS" || rr.ProviderRefundNo != "RF1" || rr.Amount != 500 {
		t.Fatal(rr, e)
	}
	if lastPath != "/ams/api/v1/payments/refund" {
		t.Fatal("refund path", lastPath)
	}
	if e := p.Close(context.Background(), QueryRequest{TradeNo: "T1"}); e != nil {
		t.Fatal("close", e)
	}
	if lastPath != "/ams/api/v1/payments/cancel" {
		t.Fatal("close path", lastPath)
	}
}

func alipaygSignWith(t *testing.T, key *rsa.PrivateKey, message string) string {
	t.Helper()
	h := crypto.SHA256.New()
	h.Write([]byte(message))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h.Sum(nil))
	if e != nil {
		t.Fatal(e)
	}
	return base64.StdEncoding.EncodeToString(sig)
}
