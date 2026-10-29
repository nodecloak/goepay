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
	"strconv"
	"strings"
	"testing"
	"time"
)

func douyinpayTestKeyPEM(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), key
}

func douyinpayPubPEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func douyinpayChannel(t *testing.T, appTypes []string, platformPEM string) (PluginChannel, *rsa.PrivateKey) {
	privPEM, key := douyinpayTestKeyPEM(t)
	certs := map[string]string{"rsa_private_key": privPEM}
	if platformPEM != "" {
		certs["platform_cert"] = platformPEM
	}
	return PluginChannel{
		ID: 15, Plugin: "douyinpay", PayType: "douyinpay", AppTypes: appTypes,
		Config:       map[string]string{"appid": "ck1", "appmchid": "M1", "apikey": "0123456789abcdef0123456789abcdef", "certserial": "SER1"},
		Certificates: certs,
	}, key
}

func TestPluginDouyinpayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("douyinpay")
	if !ok || spec.Title != "抖音支付" || !spec.Serves("douyinpay") {
		t.Fatal("douyinpay spec", ok)
	}
	c, _ := douyinpayChannel(t, nil, "")
	if _, e := BuildPluginChannel(c); e != nil {
		t.Fatal(e)
	}
	// apikey must be 32 chars
	bad, _ := douyinpayChannel(t, nil, "")
	bad.Config["apikey"] = "short"
	if _, e := BuildPluginChannel(bad); e == nil {
		t.Fatal("accepted short apikey")
	}
	// missing private key
	nokey, _ := douyinpayChannel(t, nil, "")
	delete(nokey.Certificates, "rsa_private_key")
	if _, e := BuildPluginChannel(nokey); e == nil {
		t.Fatal("accepted missing key")
	}
}

func TestPluginDouyinpayCreateNativeSignsRequest(t *testing.T) {
	var gotAuth, gotBody, reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	c, key := douyinpayChannel(t, []string{"1"}, "")
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	dp := p.(*douyinpayPlugin)
	dp.gateway = srv.URL

	reply = `{"err_no":0,"err_tips":"ok","data":{"code_url":"https://douyin/qr","prepay_id":"PP1"}}`
	r := CreateRequest{TradeNo: "T1", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100, ClientIP: "1.1.1.1"}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://douyin/qr" || res.ProviderTradeNo != "PP1" {
		t.Fatal(res, e)
	}
	// Verify the Authorization signature independently with the public key.
	if !strings.HasPrefix(gotAuth, "DouyinPay-RSA ") {
		t.Fatal("auth prefix", gotAuth)
	}
	fields := douyinpayParseAuth(gotAuth[len("DouyinPay-RSA "):])
	message := http.MethodPost + "\n" + "/v1/trade/transactions/native" + "\n" + fields["timestamp"] + "\n" + fields["nonce_str"] + "\n" + gotBody + "\n"
	if !douyinpayVerify(t, &key.PublicKey, message, fields["signature"]) {
		t.Fatal("request signature invalid")
	}
	if fields["mchid"] != "M1" || fields["serial_no"] != "SER1" {
		t.Fatal("auth fields", fields)
	}
	// business failure
	reply = `{"err_no":40001,"err_tips":"bad"}`
	var pe *ProviderError
	if _, e = p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business failure", e)
	}
	// 5xx -> unknown
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) })
	var unknown *UnknownOutcome
	if _, e = p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("5xx unknown", e)
	}
}

func TestPluginDouyinpayMethods(t *testing.T) {
	c, _ := douyinpayChannel(t, []string{"1", "3", "4"}, "")
	p, _ := BuildPluginChannel(c)
	m := strings.Join(p.Capabilities().Methods, ",")
	if !strings.Contains(m, "native") || !strings.Contains(m, "h5") || !strings.Contains(m, "app") {
		t.Fatal("methods", m)
	}
}

func TestPluginDouyinpayNotification(t *testing.T) {
	_, platformKey := douyinpayTestKeyPEM(t)
	platformPEM := douyinpayPubPEM(t, platformKey)
	c, _ := douyinpayChannel(t, nil, platformPEM)
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	dp := p.(*douyinpayPlugin)

	// Build the encrypted resource (AES-256-GCM under the apikey).
	resourcePlain := `{"trade_state":"SUCCESS","out_trade_no":"T1","transaction_id":"DY1","amount":{"total":100,"currency":"CNY"},"payer":{"openid":"o-dy"}}`
	ciphertext, nonce, aad := douyinpayEncryptResource(t, []byte("0123456789abcdef0123456789abcdef"), resourcePlain)
	bodyObj := map[string]any{"resource": map[string]any{"ciphertext": ciphertext, "nonce": nonce, "associated_data": aad}}
	body, _ := json.Marshal(bodyObj)

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nstr := "abcd"
	message := ts + "\n" + nstr + "\n" + string(body) + "\n"
	sig := douyinpaySign(t, platformKey, message)
	h := http.Header{}
	h.Set("Douyinpay-Signature", sig)
	h.Set("Douyinpay-Timestamp", ts)
	h.Set("Douyinpay-Nonce", nstr)

	ev, e := dp.VerifyNotification(context.Background(), h, body, ExpectedPayment{TradeNo: "T1", Amount: 100})
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "DY1" || ev.Amount != 100 || ev.BuyerID != "o-dy" {
		t.Fatal(ev, e)
	}
	// wrong amount
	if _, e = dp.VerifyNotification(context.Background(), h, body, ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	// bad header signature
	hbad := http.Header{}
	hbad.Set("Douyinpay-Signature", base64.StdEncoding.EncodeToString([]byte("bad")))
	hbad.Set("Douyinpay-Timestamp", ts)
	hbad.Set("Douyinpay-Nonce", nstr)
	if _, e = dp.VerifyNotification(context.Background(), hbad, body, ExpectedPayment{TradeNo: "T1", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sig", e)
	}
	// without platform key configured -> unsupported
	c2, _ := douyinpayChannel(t, nil, "")
	p2, _ := BuildPluginChannel(c2)
	if _, e = p2.(*douyinpayPlugin).VerifyNotification(context.Background(), h, body, ExpectedPayment{TradeNo: "T1", Amount: 100}); !errors.Is(e, ErrUnsupported) {
		t.Fatal("unsupported without platform key", e)
	}
	if st, _, bdy := dp.NotificationAck(nil); st != 200 || !strings.Contains(bdy, "SUCCESS") {
		t.Fatal(st, bdy)
	}
}

// ---- test crypto helpers ----

func douyinpayParseAuth(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			out[strings.TrimSpace(kv[0])] = strings.Trim(kv[1], `"`)
		}
	}
	return out
}

func douyinpaySign(t *testing.T, key *rsa.PrivateKey, message string) string {
	t.Helper()
	h := crypto.SHA256.New()
	h.Write([]byte(message))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h.Sum(nil))
	if e != nil {
		t.Fatal(e)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func douyinpayVerify(t *testing.T, key *rsa.PublicKey, message, sig string) bool {
	t.Helper()
	raw, e := base64.StdEncoding.DecodeString(sig)
	if e != nil {
		return false
	}
	h := crypto.SHA256.New()
	h.Write([]byte(message))
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, h.Sum(nil), raw) == nil
}

func douyinpayEncryptResource(t *testing.T, apikey []byte, plain string) (string, string, string) {
	t.Helper()
	nonce := "123456789012"
	aad := "ad1"
	block, e := aes.NewCipher(apikey)
	if e != nil {
		t.Fatal(e)
	}
	gcm, e := cipher.NewGCMWithNonceSize(block, len(nonce))
	if e != nil {
		t.Fatal(e)
	}
	ct := gcm.Seal(nil, []byte(nonce), []byte(plain), []byte(aad))
	return base64.StdEncoding.EncodeToString(ct), nonce, aad
}
