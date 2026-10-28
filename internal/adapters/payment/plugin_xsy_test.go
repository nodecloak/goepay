package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func xsyTestKeyPEMs(t *testing.T) (priv, pub string) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	privDER, _ := x509.MarshalPKCS8PrivateKey(key)
	pubDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
}

func xsyChannel(t *testing.T, payType string) PluginChannel {
	privPEM, pubPEM := xsyTestKeyPEMs(t)
	// Same key as both merchant private and platform public, so a callback the
	// test signs with the private key verifies against the configured public.
	return PluginChannel{
		ID: 5, Plugin: "xsy", PayType: payType,
		Config: map[string]string{"appid": "ORG9", "appmchid": "MCH9", "appkey": pubPEM, "appsecret": privPEM, "appswitch": "0"},
	}
}

func TestPluginXsyRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("xsy")
	if !ok || spec.Title != "新生易" || !spec.Serves("alipay") || spec.Serves("qqpay") {
		t.Fatal("xsy spec", ok)
	}
	if _, e := BuildPluginChannel(xsyChannel(t, "wxpay")); e != nil {
		t.Fatal(e)
	}
	bad := xsyChannel(t, "wxpay")
	delete(bad.Config, "appmchid")
	if _, e := BuildPluginChannel(bad); e == nil {
		t.Fatal("accepted missing appmchid")
	}
	if _, e := BuildPluginChannel(xsyChannel(t, "qqpay")); e == nil {
		t.Fatal("accepted unserved pay type")
	}
}

func TestPluginXsySignContent(t *testing.T) {
	// Independently constructed envelope sign string.
	env := map[string]string{"orgNo": "ORG9", "reqData": `{"a":1}`, "version": "1.0", "sign": "x", "signType": "RSA", "empty": ""}
	want := "orgNo=ORG9&reqData={\"a\":1}&signType=RSA&version=1.0"
	if got := xsySignContent(env); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPluginXsyCreate(t *testing.T) {
	var gotPath, gotBody string
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, e := BuildPluginChannel(xsyChannel(t, "alipay"))
	if e != nil {
		t.Fatal(e)
	}
	xp := p.(*xsyPlugin)
	xp.gateway = srv.URL

	// native → activeScan → QR from payUrl (qrContent wrapper stripped).
	reply = `{"code":"0000","respData":{"outOrderNo":"X1","payUrl":"https://u/x?qrContent=https%3A%2F%2Fqr.x%2F1&sign=zzz"}}`
	res, e := xp.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1000, ClientIP: "1.2.3.4"})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https%3A%2F%2Fqr.x%2F1" || res.ProviderTradeNo != "X1" {
		t.Fatal(res, e)
	}
	if gotPath != "/trade/activeScan" || !strings.Contains(gotBody, `merchantNo`) || !strings.Contains(gotBody, `MCH9`) || !strings.Contains(gotBody, `amt`) {
		t.Fatal("request", gotPath, gotBody)
	}

	// jsapi needs OpenID.
	if _, e := xp.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "s", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi without openid accepted", e)
	}
	reply = `{"code":"0000","respData":{"outOrderNo":"X2","source":"{\"tradeNO\":\"abc\"}"}}`
	res, e = xp.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "s", Method: "jsapi", OpenID: "o-1", NotifyURL: "https://pay.example/n", Amount: 1})
	if e != nil || res.MaterialType != "jsapi" || !strings.Contains(res.Material, "tradeNO") {
		t.Fatal(res, e)
	}
	if gotPath != "/trade/jsapiScan" {
		t.Fatal(gotPath)
	}

	// Business refusal.
	reply = `{"code":"9999","msg":"x"}`
	if _, e := xp.Create(context.Background(), CreateRequest{TradeNo: "T3", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.As(e, new(*ProviderError)) {
		t.Fatal("refusal not definitive", e)
	}
	// Unreadable 2xx → unknown.
	reply = `not json`
	if _, e := xp.Create(context.Background(), CreateRequest{TradeNo: "T4", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.As(e, new(*UnknownOutcome)) {
		t.Fatal("unreadable must be unknown", e)
	}
}

func TestPluginXsyNotification(t *testing.T) {
	ch := xsyChannel(t, "alipay")
	p, e := BuildPluginChannel(ch)
	if e != nil {
		t.Fatal(e)
	}
	xp := p.(*xsyPlugin)
	v := p.(NotificationVerifier)

	// Build a callback whose respData is signed by the merchant private key
	// (which equals the configured platform public key here).
	resp := map[string]any{"orderNo": "T9", "outOrderNo": "X9", "buyerId": "b-9", "tranSts": "SUCCESS", "amt": "1000"}
	respJSON, _ := xsyMarshal(resp)
	envelope := map[string]string{"respData": string(respJSON)}
	sig, e := rsaSign(xp.priv, crypto.SHA1, xsySignContent(envelope))
	if e != nil {
		t.Fatal(e)
	}
	body := `{"respData":` + string(respJSON) + `,"sign":"` + sig + `"}`
	exp := ExpectedPayment{TradeNo: "T9", Amount: 1000}
	ev, e := v.VerifyNotification(context.Background(), nil, []byte(body), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "X9" || ev.BuyerID != "b-9" || ev.Amount != 1000 {
		t.Fatal(ev, e)
	}
	// Bad sign.
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(`{"respData":`+string(respJSON)+`,"sign":"`+base64.StdEncoding.EncodeToString([]byte("no"))+`"}`), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign accepted", e)
	}
	// Wrong amount.
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(body), ExpectedPayment{TradeNo: "T9", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount accepted", e)
	}
	// Wrong trade no.
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(body), ExpectedPayment{TradeNo: "OTHER", Amount: 1000}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade accepted", e)
	}
	// Non-success state.
	resp2 := map[string]any{"orderNo": "T9", "outOrderNo": "X9", "tranSts": "FAIL", "amt": "1000"}
	rj2, _ := xsyMarshal(resp2)
	sig2, _ := rsaSign(xp.priv, crypto.SHA1, xsySignContent(map[string]string{"respData": string(rj2)}))
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(`{"respData":`+string(rj2)+`,"sign":"`+sig2+`"}`), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success accepted", e)
	}
	// Ack.
	if status, _, b := p.(NotificationAcker).NotificationAck(nil); status != 200 || !strings.Contains(b, "success") {
		t.Fatal(status, b)
	}
}
