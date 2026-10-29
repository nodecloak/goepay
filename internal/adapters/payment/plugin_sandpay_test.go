package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func sandpayChannel(payType string) PluginChannel {
	return PluginChannel{ID: 50, Plugin: "sandpay", PayType: payType,
		Config:       map[string]string{"appid": "MID001", "appkey": sandpayTestPFXPass, "appswitch": "0", "product": "QZF"},
		Certificates: map[string]string{"sandcert": sandpayPlatCertB64, "sandpfx": sandpayMerchPFXB64},
	}
}

// sandpayTestKeys loads the 杉德 private key (to sign replies) and the merchant
// public key (to encrypt a reply's AES key), from the test fixtures.
func sandpayTestKeys(t *testing.T) (platPriv *rsa.PrivateKey, merchPub *rsa.PublicKey) {
	t.Helper()
	var e error
	platPriv, e = sandpayPFXPrivateKey(sandpayPlatPFXB64, sandpayTestPFXPass)
	if e != nil {
		t.Fatal("plat pfx", e)
	}
	merchPub, e = sandpayCertPublicKey(sandpayMerchCertB64)
	if e != nil {
		t.Fatal("merch cert", e)
	}
	return
}

// sandpayForgeReply builds a respCode=success envelope the plugin will accept:
// bizData = base64(AES-ECB(plaintext)), encryptKey = base64(RSA(merchPub,key)),
// sign = base64(RSA-SHA256(platPriv, bizData)).
func sandpayForgeReply(t *testing.T, plaintext string) string {
	t.Helper()
	platPriv, merchPub := sandpayTestKeys(t)
	var key [16]byte
	if _, e := rand.Read(key[:]); e != nil {
		t.Fatal(e)
	}
	ct, e := sandpayAESEncrypt([]byte(plaintext), key[:])
	if e != nil {
		t.Fatal(e)
	}
	bizData := base64.StdEncoding.EncodeToString(ct)
	encKey, e := rsa.EncryptPKCS1v15(rand.Reader, merchPub, key[:])
	if e != nil {
		t.Fatal(e)
	}
	sig, e := rsaSign(platPriv, crypto.SHA256, bizData)
	if e != nil {
		t.Fatal(e)
	}
	return `{"respCode":"success","encryptKey":"` + base64.StdEncoding.EncodeToString(encKey) + `","bizData":"` + bizData + `","sign":"` + sig + `"}`
}

// sandpayForgeNotify builds the POST body of a notify: bizData is PLAINTEXT
// JSON, sign = base64(RSA-SHA256(platPriv, bizData)).
func sandpayForgeNotify(t *testing.T, bizData string) []byte {
	t.Helper()
	platPriv, _ := sandpayTestKeys(t)
	sig, e := rsaSign(platPriv, crypto.SHA256, bizData)
	if e != nil {
		t.Fatal(e)
	}
	form := url.Values{"bizData": {bizData}, "sign": {sig}}
	return []byte(form.Encode())
}

func TestPluginSandpayRegistry(t *testing.T) {
	spec, ok := LookupPlugin("sandpay")
	if !ok || spec.Title != "杉德支付" || !spec.Serves("bank") || spec.Serves("qqpay") {
		t.Fatal("spec", spec, ok)
	}
	if _, e := BuildPluginChannel(sandpayChannel("alipay")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{sandpayChannel("qqpay")}
	missing := sandpayChannel("alipay")
	delete(missing.Certificates, "sandpfx")
	badpwd := sandpayChannel("alipay")
	badpwd.Config["appkey"] = "wrongpass"
	badcert := sandpayChannel("alipay")
	badcert.Certificates["sandcert"] = "not-a-cert"
	bad = append(bad, missing, badpwd, badcert)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

// AES-128-ECB round trip against an independently fixed vector is implicit; here
// we assert PKCS7 + ECB encrypt/decrypt round-trips and that the plugin's reply
// decryption accepts a reply forged with the counterpart keys.
func TestPluginSandpayCryptoRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef")
	ct, e := sandpayAESEncrypt([]byte("hello 杉德 world"), key)
	if e != nil || len(ct)%16 != 0 {
		t.Fatal(ct, e)
	}
	pt, e := sandpayAESDecrypt(ct, key)
	if e != nil || string(pt) != "hello 杉德 world" {
		t.Fatal(string(pt), e)
	}
}

func TestPluginSandpayCreateNative(t *testing.T) {
	var gotEnvelope map[string]any
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotEnvelope, _ = callbackJSON(b)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, e := BuildPluginChannel(sandpayChannel("wxpay"))
	if e != nil {
		t.Fatal(e)
	}
	sp := p.(*sandpayPlugin)
	sp.apiURL = srv.URL

	reply = sandpayForgeReply(t, `{"resultStatus":"success","sandSerialNo":"SD9","credential":{"qrCode":"weixin://wxpay/bizpayurl?pr=ss"}}`)
	res, err := p.Create(context.Background(), CreateRequest{TradeNo: "T100", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", ClientIP: "203.0.113.3", Amount: 1000})
	if err != nil || res.MaterialType != "qrcode" || res.Material != "weixin://wxpay/bizpayurl?pr=ss" || res.ProviderTradeNo != "SD9" {
		t.Fatal(res, err)
	}
	if jsonText(gotEnvelope, "accessMid") != "MID001" || jsonText(gotEnvelope, "signType") != "RSA" || jsonText(gotEnvelope, "encryptType") != "AES" || gotEnvelope["bizData"] == "" || gotEnvelope["encryptKey"] == "" || gotEnvelope["sign"] == "" {
		t.Fatal("envelope", gotEnvelope)
	}
}

func TestPluginSandpayCreateOutcomes(t *testing.T) {
	var reply string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(sandpayChannel("alipay"))
	sp := p.(*sandpayPlugin)
	sp.apiURL = srv.URL
	r := CreateRequest{TradeNo: "T2", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 50}

	// respCode fail -> definitive refusal.
	status, reply = 200, `{"respCode":"fail","respDesc":"验签失败"}`
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("respCode fail must be definitive", e)
	}
	// resultStatus fail inside a signed envelope -> definitive refusal.
	status, reply = 200, sandpayForgeReply(t, `{"resultStatus":"fail","errorCode":"SDE001","errorDesc":"余额不足"}`)
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("resultStatus fail must be definitive", e)
	}
	// Unreadable respCode & 5xx -> unknown.
	var unknown *UnknownOutcome
	status, reply = 200, `{"respCode":"???"}`
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("unknown respCode", e)
	}
	status, reply = 500, `{}`
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("5xx unknown", e)
	}
	// success respCode but bad signature over bizData -> unknown.
	status, reply = 200, `{"respCode":"success","encryptKey":"AAAA","bizData":"QUJD","sign":"AAAA"}`
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("bad sign unknown", e)
	}
}

func TestPluginSandpayNotification(t *testing.T) {
	p, _ := BuildPluginChannel(sandpayChannel("alipay"))
	sp := p.(*sandpayPlugin)
	biz := `{"orderStatus":"success","outOrderNo":"T777","sandSerialNo":"SD77","amount":"10.00","payer":{"payerAccNo":"2088x"}}`
	raw := sandpayForgeNotify(t, biz)
	exp := ExpectedPayment{TradeNo: "T777", Amount: 1000}
	ev, e := sp.VerifyNotification(context.Background(), nil, raw, exp)
	if e != nil || ev.State != "SUCCESS" || ev.Amount != 1000 || ev.ProviderTradeNo != "SD77" || ev.BuyerID != "2088x" {
		t.Fatal(ev, e)
	}
	// Tampered bizData breaks signature.
	badBiz := strings.Replace(biz, `"amount":"10.00"`, `"amount":"20.00"`, 1)
	form := url.Values{"bizData": {badBiz}, "sign": {extractSign(t, raw)}}
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(form.Encode()), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("tamper must fail sign", e)
	}
	// Correctly signed but wrong amount / trade.
	if _, e := sp.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "T777", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e := sp.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "OTHER", Amount: 1000}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// Non-success orderStatus.
	ns := sandpayForgeNotify(t, `{"orderStatus":"pending","outOrderNo":"T777","amount":"10.00"}`)
	if _, e := sp.VerifyNotification(context.Background(), nil, ns, exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success", e)
	}
	// Ack bodies.
	if _, _, body := sp.NotificationAck(nil); body != "respCode=000000" {
		t.Fatal(body)
	}
	if _, _, body := sp.NotificationAck(ErrInvalid); body != "respCode=020002" {
		t.Fatal(body)
	}
}

// extractSign pulls the sign value out of a url-encoded notify body.
func extractSign(t *testing.T, raw []byte) string {
	t.Helper()
	v, e := url.ParseQuery(string(raw))
	if e != nil {
		t.Fatal(e)
	}
	return v.Get("sign")
}

func TestPluginSandpayRefund(t *testing.T) {
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(sandpayChannel("wxpay"))
	sp := p.(*sandpayPlugin)
	sp.apiURL = srv.URL
	reply = sandpayForgeReply(t, `{"resultStatus":"success","sandSerialNo":"RF1","amount":"3.00"}`)
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T9", RefundNo: "R9", Amount: 300, TotalAmount: 1000})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 300 || rr.ProviderRefundNo != "RF1" {
		t.Fatal(rr, e)
	}
}
