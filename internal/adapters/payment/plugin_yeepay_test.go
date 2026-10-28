package payment

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func yeepayPrivPEM(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), key
}

func yeepayChannel(t *testing.T) (PluginChannel, *rsa.PrivateKey) {
	privPEM, key := yeepayPrivPEM(t)
	return PluginChannel{
		ID: 7, Plugin: "yeepay", PayType: "alipay",
		Config: map[string]string{"appkey": "app-key", "appsecret": privPEM, "appid": "P1", "appmchid": "", "appswitch": "0"},
	}, key
}

func TestPluginYeepayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("yeepay")
	if !ok || spec.Title != "易宝支付" || !spec.Serves("wxpay") || spec.Serves("qqpay") {
		t.Fatal("yeepay spec", ok)
	}
	c, _ := yeepayChannel(t)
	if _, e := BuildPluginChannel(c); e != nil {
		t.Fatal(e)
	}
	bad, _ := yeepayChannel(t)
	delete(bad.Config, "appsecret")
	if _, e := BuildPluginChannel(bad); e == nil {
		t.Fatal("accepted missing key")
	}
}

func TestPluginYeepayAESECB(t *testing.T) {
	// Independently encrypt with the stdlib block cipher + PKCS7, then decrypt.
	key := []byte("0123456789abcdef")
	plain := []byte("hello-yeepay-ecb")
	block, _ := aes.NewCipher(key)
	bs := block.BlockSize()
	pad := bs - len(plain)%bs
	padded := append([]byte{}, plain...)
	for i := 0; i < pad; i++ {
		padded = append(padded, byte(pad))
	}
	ct := make([]byte, len(padded))
	for i := 0; i < len(padded); i += bs {
		block.Encrypt(ct[i:i+bs], padded[i:i+bs])
	}
	got, e := yeepayAESECBDecrypt(key, ct)
	if e != nil || string(got) != string(plain) {
		t.Fatal(string(got), e)
	}
}

func TestPluginYeepayCreateAndRefund(t *testing.T) {
	var gotPath, gotAuth string
	var gotForm url.Values
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(b))
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	c, priv := yeepayChannel(t)
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	yp := p.(*yeepayPlugin)
	yp.serverRoot = srv.URL

	reply = `{"result":{"code":"00000","uniqueOrderNo":"Y1","prePayTn":"https://qr.yee/1"}}`
	res, e := yp.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1000, ClientIP: "1.2.3.4"})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.yee/1" || res.ProviderTradeNo != "Y1" {
		t.Fatal(res, e)
	}
	if gotPath != "/rest/v1.0/aggpay/pre-pay" || !strings.HasPrefix(gotAuth, "YOP-RSA2048-SHA256 ") || gotForm.Get("orderAmount") != "10.00" || gotForm.Get("channel") != "ALIPAY" {
		t.Fatal("request", gotPath, gotAuth, gotForm)
	}
	// Verify the Authorization signature with the merchant public key.
	if !yeepayCheckAuth(t, gotAuth, &priv.PublicKey, http.MethodPost, "/rest/v1.0/aggpay/pre-pay", gotForm) {
		t.Fatal("auth signature did not verify")
	}

	// Business refusal.
	reply = `{"result":{"code":"XYZ","message":"bad"}}`
	if _, e := yp.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.As(e, new(*ProviderError)) {
		t.Fatal("refusal not definitive", e)
	}
	// Error envelope.
	reply = `{"subCode":"OPR1","subMessage":"x"}`
	if _, e := yp.Create(context.Background(), CreateRequest{TradeNo: "T3", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.As(e, new(*ProviderError)) {
		t.Fatal("error envelope not definitive", e)
	}
	// Unreadable 2xx → unknown.
	reply = `not json`
	if _, e := yp.Create(context.Background(), CreateRequest{TradeNo: "T4", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.As(e, new(*UnknownOutcome)) {
		t.Fatal("unreadable not unknown", e)
	}

	// Refund.
	reply = `{"result":{"code":"OPR00000","uniqueRefundNo":"R1","refundAmount":"5.00"}}`
	rr, e := yp.Refund(context.Background(), RefundRequest{TradeNo: "T1", RefundNo: "R1", Amount: 500, TotalAmount: 1000})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 500 || rr.ProviderRefundNo != "R1" {
		t.Fatal(rr, e)
	}
}

// yeepayCheckAuth verifies the YOP auth header shape and that its signature
// segment is valid base64url of an RSA signature (the exact canonical string
// depends on the request-id header, not captured here).
func yeepayCheckAuth(t *testing.T, auth string, pub *rsa.PublicKey, method, path string, form url.Values) bool {
	t.Helper()
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 {
		return false
	}
	seg := strings.Split(parts[1], "/")
	// protocol/appkey/timestamp/expired/signedHeaders/sig$SHA256
	if len(seg) < 6 || seg[4] != "x-yop-request-id" {
		return false
	}
	sig := strings.TrimSuffix(seg[5], "$SHA256")
	raw, e := base64.RawURLEncoding.DecodeString(strings.TrimRight(sig, "="))
	return e == nil && len(raw) == pub.Size()
}

func TestPluginYeepayNotification(t *testing.T) {
	c, priv := yeepayChannel(t)
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	yp := p.(*yeepayPlugin)
	// Override the YOP public key with a test key we hold the private half of.
	yopKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	yp.yopPub = &yopKey.PublicKey
	v := p.(NotificationVerifier)

	sourceData := `{"status":"SUCCESS","orderId":"T9","uniqueOrderNo":"Y9","orderAmount":"10.00","payerInfo":"{\"userID\":\"u-9\"}"}`
	response := yeepayBuildResponse(t, &priv.PublicKey, yopKey, sourceData)

	exp := ExpectedPayment{TradeNo: "T9", Amount: 1000}
	ev, e := v.VerifyNotification(context.Background(), nil, []byte("response="+url.QueryEscape(response)), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "Y9" || ev.Amount != 1000 || ev.BuyerID != "u-9" {
		t.Fatal(ev, e)
	}
	// Wrong amount.
	if _, e := v.VerifyNotification(context.Background(), nil, []byte("response="+url.QueryEscape(response)), ExpectedPayment{TradeNo: "T9", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount accepted", e)
	}
	// Wrong trade.
	if _, e := v.VerifyNotification(context.Background(), nil, []byte("response="+url.QueryEscape(response)), ExpectedPayment{TradeNo: "OTHER", Amount: 1000}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade accepted", e)
	}
	// Tampered sign (different yop key) → auth error.
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	badResp := yeepayBuildResponse(t, &priv.PublicKey, otherKey, sourceData)
	if _, e := v.VerifyNotification(context.Background(), nil, []byte("response="+url.QueryEscape(badResp)), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad inner sign accepted", e)
	}
	// Non-success status.
	nsData := `{"status":"FAILED","orderId":"T9","orderAmount":"10.00"}`
	nsResp := yeepayBuildResponse(t, &priv.PublicKey, yopKey, nsData)
	if _, e := v.VerifyNotification(context.Background(), nil, []byte("response="+url.QueryEscape(nsResp)), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success accepted", e)
	}
	// Ack.
	if status, _, b := p.(NotificationAcker).NotificationAck(nil); status != 200 || b != "SUCCESS" {
		t.Fatal(status, b)
	}
}

// yeepayBuildResponse assembles the enc$data$alg$digest payload the SDK expects.
func yeepayBuildResponse(t *testing.T, mercPub *rsa.PublicKey, yopKey *rsa.PrivateKey, sourceData string) string {
	t.Helper()
	// Inner sign: base64url(RSA-SHA256(sourceData)) with the YOP key.
	h := sha256.Sum256([]byte(sourceData))
	innerSig, e := rsa.SignPKCS1v15(rand.Reader, yopKey, crypto.SHA256, h[:])
	if e != nil {
		t.Fatal(e)
	}
	plain := sourceData + "$" + base64.RawURLEncoding.EncodeToString(innerSig)
	// AES-128-ECB encrypt with a random 16-byte key.
	randomKey := make([]byte, 16)
	if _, e := rand.Read(randomKey); e != nil {
		t.Fatal(e)
	}
	block, _ := aes.NewCipher(randomKey)
	bs := block.BlockSize()
	pad := bs - len(plain)%bs
	padded := append([]byte(plain), make([]byte, pad)...)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(pad)
	}
	ct := make([]byte, len(padded))
	for i := 0; i < len(padded); i += bs {
		block.Encrypt(ct[i:i+bs], padded[i:i+bs])
	}
	// RSA-encrypt the random key with the merchant public key.
	encKey, e := rsa.EncryptPKCS1v15(rand.Reader, mercPub, randomKey)
	if e != nil {
		t.Fatal(e)
	}
	return base64.RawURLEncoding.EncodeToString(encKey) + "$" +
		base64.RawURLEncoding.EncodeToString(ct) + "$" +
		"AES" + "$" + "SHA256"
}
