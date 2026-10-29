package payment

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
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
	"testing"
)

func wxpayngKeyPEM(t *testing.T) (privPEM, pubPEM string, priv *rsa.PrivateKey) {
	t.Helper()
	k, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	privPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	pubDER, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	pubPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	return privPEM, pubPEM, k
}

// wxpayngChannel returns a channel plus the platform private key the test uses
// to sign callbacks (the plugin holds the matching platform public key).
func wxpayngChannel(t *testing.T, appTypes []string) (PluginChannel, *rsa.PrivateKey) {
	merchPriv, _, _ := wxpayngKeyPEM(t)
	_, platPub, platPriv := wxpayngKeyPEM(t)
	return PluginChannel{ID: 9, Plugin: "wxpayng", PayType: "wxpay",
		Config: map[string]string{
			"appid": "wx123", "appmchid": "100000", "appsecret": "01234567890123456789012345678901",
			"appkey": "SERIAL01", "appurl": "4111", "platform_serial": "PUB_KEY_ID_1",
		},
		Certificates: map[string]string{"merchant_private_key": merchPriv, "platform_public_key": platPub},
		AppTypes:     appTypes,
	}, platPriv
}

func TestPluginWxpayngRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("wxpayng")
	if !ok || spec.Title != "微信支付国际版V3" || !spec.Serves("wxpay") || spec.Serves("alipay") {
		t.Fatal("spec", spec, ok)
	}
	c, _ := wxpayngChannel(t, []string{"1"})
	if _, e := BuildPluginChannel(c); e != nil {
		t.Fatal(e)
	}
	// Wrong-length APIv3 key.
	badKey, _ := wxpayngChannel(t, []string{"1"})
	badKey.Config["appsecret"] = "tooshort"
	// Missing private key.
	missing, _ := wxpayngChannel(t, []string{"1"})
	delete(missing.Certificates, "merchant_private_key")
	// Unserved pay type.
	unserved, _ := wxpayngChannel(t, []string{"1"})
	unserved.PayType = "alipay"
	for i, bad := range []PluginChannel{badKey, missing, unserved} {
		if _, e := BuildPluginChannel(bad); e == nil {
			t.Fatal("accepted bad", i)
		}
	}
}

func wxpayngTestPlugin(t *testing.T, host string, appTypes []string) (*wxpayngPlugin, *rsa.PrivateKey) {
	c, platPriv := wxpayngChannel(t, appTypes)
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	return p.(*wxpayngPlugin), platPriv
}

func TestPluginWxpayngCreateMethods(t *testing.T) {
	var lastPath, lastAuth string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		lastAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = nil
		json.Unmarshal(b, &body)
		switch r.URL.Path {
		case "/v3/global/transactions/native":
			io.WriteString(w, `{"code_url":"weixin://wxpay/bizpayurl?pr=abc"}`)
		case "/v3/global/transactions/mweb":
			io.WriteString(w, `{"mweb_url":"https://wx.tenpay.com/mweb"}`)
		case "/v3/global/transactions/jsapi", "/v3/global/transactions/app":
			io.WriteString(w, `{"prepay_id":"pp_1"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	p, _ := wxpayngTestPlugin(t, srv.URL, []string{"1", "2", "3", "5"})
	p.pluginBase.plugin = "wxpayng"
	// point execute at the test server
	patchWxHost(p, srv.URL)

	r := CreateRequest{TradeNo: "T1", Subject: "Order", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", ClientIP: "1.2.3.4", Amount: 100}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "weixin://wxpay/bizpayurl?pr=abc" {
		t.Fatal("native", res, e)
	}
	if lastPath != "/v3/global/transactions/native" || lastAuth == "" {
		t.Fatal("native request", lastPath, lastAuth)
	}
	amt := body["amount"].(map[string]any)
	if amt["currency"] != "CNY" {
		t.Fatal("currency", amt)
	}

	r.Method = "h5"
	if res, e = p.Create(context.Background(), r); e != nil || res.MaterialType != "jump" || res.Material != "https://wx.tenpay.com/mweb" {
		t.Fatal("h5", res, e)
	}

	r.Method = "jsapi"
	r.OpenID = "o-abc"
	if res, e = p.Create(context.Background(), r); e != nil || res.MaterialType != "jsapi" {
		t.Fatal("jsapi", res, e)
	}
	var js map[string]any
	if json.Unmarshal([]byte(res.Material), &js) != nil || js["package"] != "prepay_id=pp_1" || js["signType"] != "RSA" || js["paySign"] == "" {
		t.Fatal("jsapi params", res.Material)
	}

	r.Method = "app"
	r.OpenID = ""
	if res, e = p.Create(context.Background(), r); e != nil || res.MaterialType != "app" {
		t.Fatal("app", res, e)
	}
	var ap map[string]any
	if json.Unmarshal([]byte(res.Material), &ap) != nil || ap["package"] != "Sign=WXPay" || ap["prepayid"] != "pp_1" {
		t.Fatal("app params", res.Material)
	}

	// JSAPI without openid is refused offline.
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "jsapi", NotifyURL: r.NotifyURL, Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi needs openid", e)
	}
}

func TestPluginWxpayngCreateOutcomes(t *testing.T) {
	var status int
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := wxpayngTestPlugin(t, srv.URL, []string{"1"})
	patchWxHost(p, srv.URL)
	r := CreateRequest{TradeNo: "T1", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100}
	// Business error (WeChat {code,message}).
	status, reply = 400, `{"code":"PARAM_ERROR","message":"bad"}`
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business error", e)
	}
	// 2xx without code_url and without an error code -> unknown.
	status, reply = 200, `{"something":"else"}`
	var unknown *UnknownOutcome
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("missing code_url", e)
	}
	// 5xx -> unknown.
	status, reply = 500, `oops`
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("5xx", e)
	}
}

func TestPluginWxpayngVerifyNotification(t *testing.T) {
	p, platPriv := wxpayngTestPlugin(t, "https://unused", []string{"1"})
	// Encrypt a transaction resource with the APIv3 key (AEAD_AES_256_GCM).
	tx := `{"trade_state":"SUCCESS","out_trade_no":"T202610290001","id":"4200001","amount":{"total":100,"currency":"CNY"},"payer":{"openid":"o-abc"}}`
	resource := wxpayngEncryptResource(t, p.apiV3Key, []byte(tx))
	env, _ := json.Marshal(map[string]any{"id": "evt1", "resource": resource})
	ts := "1730000000"
	nonce := "noncestr"
	message := ts + "\n" + nonce + "\n" + string(env) + "\n"
	sig := wxpayngSignWith(t, platPriv, message)
	hdr := wxpayngNotifHeader(ts, nonce, "PUB_KEY_ID_1", sig)
	exp := ExpectedPayment{TradeNo: "T202610290001", Amount: 100}
	ev, e := p.VerifyNotification(context.Background(), hdr, env, exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "4200001" || ev.BuyerID != "o-abc" || ev.Amount != 100 || ev.Currency != "CNY" {
		t.Fatal(ev, e)
	}
	// Bad signature.
	badSig := wxpayngNotifHeader(ts, nonce, "PUB_KEY_ID_1", "bad")
	if _, e := p.VerifyNotification(context.Background(), badSig, env, exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// Wrong serial.
	badSerial := wxpayngNotifHeader(ts, nonce, "OTHER_SERIAL", sig)
	if _, e := p.VerifyNotification(context.Background(), badSerial, env, exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("serial", e)
	}
	// Wrong amount / order (valid signature + decrypt).
	if _, e := p.VerifyNotification(context.Background(), hdr, env, ExpectedPayment{TradeNo: "T202610290001", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e := p.VerifyNotification(context.Background(), hdr, env, ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("order", e)
	}
	// Non-success trade_state.
	tx2 := `{"trade_state":"NOTPAY","out_trade_no":"T202610290001","id":"x","amount":{"total":100,"currency":"CNY"}}`
	res2 := wxpayngEncryptResource(t, p.apiV3Key, []byte(tx2))
	env2, _ := json.Marshal(map[string]any{"resource": res2})
	msg2 := ts + "\n" + nonce + "\n" + string(env2) + "\n"
	hdr2 := wxpayngNotifHeader(ts, nonce, "PUB_KEY_ID_1", wxpayngSignWith(t, platPriv, msg2))
	if _, e := p.VerifyNotification(context.Background(), hdr2, env2, exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success", e)
	}
	// Ack bodies.
	if s, _, b := p.NotificationAck(nil); s != 200 || b != `{"code":"SUCCESS"}` {
		t.Fatal(s, b)
	}
	if s, _, _ := p.NotificationAck(errors.New("x")); s != 400 {
		t.Fatal("fail ack", s)
	}
}

func TestPluginWxpayngQueryCloseRefund(t *testing.T) {
	var lastPath, lastMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		lastMethod = r.Method
		switch {
		case r.URL.Path == "/v3/global/transactions/out-trade-no/T9":
			io.WriteString(w, `{"out_trade_no":"T9","id":"4200009","trade_state":"SUCCESS","amount":{"total":990},"payer":{"openid":"o-x"}}`)
		case r.URL.Path == "/v3/global/transactions/out-trade-no/T9/close":
			io.WriteString(w, ``)
		case r.URL.Path == "/v3/global/refunds":
			io.WriteString(w, `{"refund_id":"RF9","out_refund_no":"R9","status":"SUCCESS"}`)
		case r.URL.Path == "/v3/global/refunds/out-refund-no/R9":
			io.WriteString(w, `{"refund_id":"RF9","out_refund_no":"R9","status":"SUCCESS","amount":{"refund":500}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	p, _ := wxpayngTestPlugin(t, srv.URL, []string{"1"})
	patchWxHost(p, srv.URL)

	q, e := p.Query(context.Background(), QueryRequest{TradeNo: "T9"})
	if e != nil || q.State != "SUCCESS" || q.Amount != 990 || q.ProviderTradeNo != "4200009" || lastMethod != http.MethodGet {
		t.Fatal(q, e, lastMethod)
	}
	if e := p.Close(context.Background(), QueryRequest{TradeNo: "T9"}); e != nil {
		t.Fatal("close", e)
	}
	if lastPath != "/v3/global/transactions/out-trade-no/T9/close" {
		t.Fatal("close path", lastPath)
	}
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T9", ProviderTradeNo: "4200009", RefundNo: "R9", Amount: 500, TotalAmount: 990})
	if e != nil || rr.State != "SUCCESS" || rr.ProviderRefundNo != "RF9" || rr.Amount != 500 {
		t.Fatal(rr, e)
	}
	rq, e := p.QueryRefund(context.Background(), RefundRequest{RefundNo: "R9"})
	if e != nil || rq.State != "SUCCESS" || rq.Amount != 500 {
		t.Fatal(rq, e)
	}
}

// --- test crypto helpers (independent of the plugin's own sign/verify) ---

func wxpayngSignWith(t *testing.T, key *rsa.PrivateKey, message string) string {
	t.Helper()
	h := crypto.SHA256.New()
	h.Write([]byte(message))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h.Sum(nil))
	if e != nil {
		t.Fatal(e)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func wxpayngEncryptResource(t *testing.T, key []byte, plain []byte) map[string]any {
	t.Helper()
	nonce := []byte("123456789012") // 12 bytes
	ad := "transaction"
	block, e := aes.NewCipher(key)
	if e != nil {
		t.Fatal(e)
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		t.Fatal(e)
	}
	ct := gcm.Seal(nil, nonce, plain, []byte(ad))
	return map[string]any{
		"algorithm":       "AEAD_AES_256_GCM",
		"nonce":           string(nonce),
		"associated_data": ad,
		"ciphertext":      base64.StdEncoding.EncodeToString(ct),
	}
}

func wxpayngNotifHeader(ts, nonce, serial, sig string) http.Header {
	h := http.Header{}
	h.Set("Wechatpay-Timestamp", ts)
	h.Set("Wechatpay-Nonce", nonce)
	h.Set("Wechatpay-Serial", serial)
	h.Set("Wechatpay-Signature", sig)
	return h
}

// patchWxHost rewrites the plugin gateway constant for tests by swapping the
// package-level target. wxpayng uses the wxpayngGateway const, so we route
// requests through a host-rewriting client instead of changing the const.
func patchWxHost(p *wxpayngPlugin, host string) {
	p.host = host
}
