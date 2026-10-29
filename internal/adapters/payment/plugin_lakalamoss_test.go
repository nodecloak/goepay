package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
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

// lakalamossEnv holds a controllable key pair pair for the envelope: a merchant
// key (plugin's private key) and a platform key (plugin's public key, whose
// private half the test holds to act as the upstream).
type lakalamossEnv struct {
	merchant *rsa.PrivateKey
	platform *rsa.PrivateKey
}

func lakalamossSetup(t *testing.T) (*lakalamossPlugin, *lakalamossEnv) {
	t.Helper()
	merchant, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	base, e := newPluginBase(PluginChannel{Plugin: "lakalamoss", PayType: "alipay"}, pluginAccount("https://moss.lakala.com", "M1"))
	if e != nil {
		t.Fatal(e)
	}
	p := &lakalamossPlugin{pluginBase: base, appid: "APP1", mchid: "M1", privateKey: merchant, platformKey: &platform.PublicKey, gateway: lakalamossGateway}
	return p, &lakalamossEnv{merchant: merchant, platform: platform}
}

// serverDecrypt reverses encryptRequest: base64-decode, chunk by platform size,
// decrypt each with the platform private key.
func (env *lakalamossEnv) serverDecrypt(t *testing.T, encoded string) []byte {
	raw, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil {
		t.Fatal(e)
	}
	size := env.platform.Size()
	var out []byte
	for len(raw) > 0 {
		dec, e := rsa.DecryptPKCS1v15(rand.Reader, env.platform, raw[:size])
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, dec...)
		raw = raw[size:]
	}
	return out
}

// serverEncrypt mirrors the plugin's encryptRequest but with the merchant
// public key (so the plugin can decrypt with its private key).
func (env *lakalamossEnv) serverEncrypt(t *testing.T, data []byte) string {
	pub := &env.merchant.PublicKey
	chunk := pub.Size() - 11
	var out []byte
	for len(data) > 0 {
		n := chunk
		if n > len(data) {
			n = len(data)
		}
		enc, e := rsa.EncryptPKCS1v15(rand.Reader, pub, data[:n])
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, enc...)
		data = data[n:]
	}
	return base64.StdEncoding.EncodeToString(out)
}

// serverSign signs the sign content with the platform private key (RSA-SHA1).
func (env *lakalamossEnv) serverSign(t *testing.T, params map[string]any) string {
	content, e := lakalamossSignContent(params)
	if e != nil {
		t.Fatal(e)
	}
	h := sha1.Sum([]byte(content))
	sig, e := rsa.SignPKCS1v15(rand.Reader, env.platform, crypto.SHA1, h[:])
	if e != nil {
		t.Fatal(e)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func lakalamossChannelCerts(t *testing.T, key *rsa.PrivateKey) map[string]string {
	t.Helper()
	der, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	return map[string]string{"appsecret": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))}
}

func TestPluginLakalamossRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("lakalamoss")
	if !ok || spec.Title != "拉卡拉MOSS" || !spec.Serves("alipay") || spec.Serves("qqpay") {
		t.Fatal("lakalamoss spec", spec, ok)
	}
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	c := PluginChannel{ID: 12, Plugin: "lakalamoss", PayType: "alipay", Config: map[string]string{"appid": "APP1", "appmchid": "M1"}, Certificates: lakalamossChannelCerts(t, key)}
	if _, e := BuildPluginChannel(c); e != nil {
		t.Fatal(e)
	}
	// Missing required private key.
	miss := c
	miss.Certificates = map[string]string{}
	if _, e := BuildPluginChannel(miss); e == nil {
		t.Fatal("accepted missing key")
	}
	// Unserved pay type.
	bad := c
	bad.PayType = "qqpay"
	if _, e := BuildPluginChannel(bad); e == nil {
		t.Fatal("accepted unserved type")
	}
}

func TestPluginLakalamossSignContent(t *testing.T) {
	// getSignContent: sort keys, skip sign, nested object ksort'd + JSON (no
	// HTML escaping). Verify a known shape deterministically.
	params := map[string]any{
		"b":    "2",
		"a":    "1",
		"sign": "ignored",
		"head": map[string]any{"z": "26", "a": "1"},
	}
	got, e := lakalamossSignContent(params)
	if e != nil {
		t.Fatal(e)
	}
	want := `a=1&b=2&head={"a":"1","z":"26"}`
	if got != want {
		t.Fatalf("sign content\n got=%q\nwant=%q", got, want)
	}
}

func TestPluginLakalamossCreate(t *testing.T) {
	p, env := lakalamossSetup(t)
	var gotRequest map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var params map[string]any
		_ = json.Unmarshal(b, &params)
		// decrypt and record the request
		enc, _ := params["requestEncrypted"].(string)
		plain := env.serverDecrypt(t, enc)
		_ = json.Unmarshal(plain, &gotRequest)
		// build an encrypted, signed success response
		respObj := map[string]any{"trade_state": "SUCCESS", "pay_serial": "U1", "acc_resp_fields": map[string]any{"code": "https://qr.example/x"}}
		respRaw, _ := json.Marshal(respObj)
		head := map[string]any{"code": "000000", "desc": "success"}
		out := map[string]any{"head": head, "responseEncrypted": env.serverEncrypt(t, respRaw)}
		out["sign"] = env.serverSign(t, out)
		body, _ := json.Marshal(out)
		w.Write(body)
	}))
	defer srv.Close()
	p.gateway = srv.URL

	if got := p.Capabilities().Methods; len(got) != 2 {
		t.Fatal(got)
	}
	r := CreateRequest{TradeNo: "T1", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", ClientIP: "1.2.3.4", Amount: 1000}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.example/x" || res.ProviderTradeNo != "U1" {
		t.Fatal(res, e)
	}
	// The decrypted request carried mer_no, amount in fen, account_type.
	if gotRequest["mer_no"] != "M1" || gotRequest["total_amount"] != "1000" || gotRequest["account_type"] != "ALIPAY" || gotRequest["trans_type"] != "41" {
		t.Fatal("request", gotRequest)
	}
}

func TestPluginLakalamossCreateRefusalUnknown(t *testing.T) {
	p, _ := lakalamossSetup(t)
	// Business refusal: head.code != 000000.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"head":{"code":"000001","desc":"fail"}}`)
	}))
	defer srv.Close()
	p.gateway = srv.URL
	var pe *ProviderError
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 10}); !errors.As(e, &pe) {
		t.Fatal("business refusal", e)
	}
	// Unknown outcome: 5xx.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
	}))
	defer srv2.Close()
	p.gateway = srv2.URL
	var unknown *UnknownOutcome
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 10}); !errors.As(e, &unknown) {
		t.Fatal("unknown outcome", e)
	}
}

func TestPluginLakalamossNotification(t *testing.T) {
	p, env := lakalamossSetup(t)
	// Build a signed+encrypted notify envelope the way the upstream would.
	inner := map[string]any{"trade_state": "SUCCESS", "order_no": "T1", "pay_serial": "U1", "total_amount": "100", "user_id2": "o-x"}
	innerRaw, _ := json.Marshal(inner)
	envlp := map[string]any{"head": map[string]any{"code": "000000"}, "requestEncrypted": env.serverEncrypt(t, innerRaw)}
	envlp["sign"] = env.serverSign(t, envlp)
	body, _ := json.Marshal(envlp)

	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := p.VerifyNotification(context.Background(), nil, body, exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "U1" || ev.BuyerID != "o-x" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// Tampered sign.
	bad := map[string]any{}
	_ = json.Unmarshal(body, &bad)
	bad["sign"] = base64.StdEncoding.EncodeToString([]byte("nope"))
	badBody, _ := json.Marshal(bad)
	if _, e = p.VerifyNotification(context.Background(), nil, badBody, exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// Wrong amount.
	if _, e = p.VerifyNotification(context.Background(), nil, body, ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	// Wrong trade number.
	if _, e = p.VerifyNotification(context.Background(), nil, body, ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// Non-success status.
	ns := map[string]any{"trade_state": "FAIL", "order_no": "T1", "total_amount": "100"}
	nsRaw, _ := json.Marshal(ns)
	nsEnv := map[string]any{"head": map[string]any{"code": "000000"}, "requestEncrypted": env.serverEncrypt(t, nsRaw)}
	nsEnv["sign"] = env.serverSign(t, nsEnv)
	nsBody, _ := json.Marshal(nsEnv)
	if _, e = p.VerifyNotification(context.Background(), nil, nsBody, exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("status", e)
	}
}

func TestPluginLakalamossRefund(t *testing.T) {
	p, env := lakalamossSetup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respRaw, _ := json.Marshal(map[string]any{"result": "ok"})
		out := map[string]any{"head": map[string]any{"code": "000000"}, "responseEncrypted": env.serverEncrypt(t, respRaw)}
		out["sign"] = env.serverSign(t, out)
		body, _ := json.Marshal(out)
		w.Write(body)
	}))
	defer srv.Close()
	p.gateway = srv.URL
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", RefundNo: "R1", Amount: 50, TotalAmount: 100})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 50 {
		t.Fatal(rr, e)
	}
}
