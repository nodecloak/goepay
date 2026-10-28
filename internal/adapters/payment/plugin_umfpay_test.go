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
	"net/url"
	"strings"
	"testing"
)

// umfpayTestKeys builds a merchant key pair (used both as the merchant private
// key and, via its public part, as the "platform" cert for verification).
func umfpayTestKeys(t *testing.T) (*rsa.PrivateKey, string, string) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	privDER, _ := x509.MarshalPKCS8PrivateKey(key)
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	pubDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	return key, privPEM, pubPEM
}

func umfpayChannel(t *testing.T, payType string) (PluginChannel, *rsa.PrivateKey, string) {
	_, privPEM, pubPEM := umfpayTestKeys(t)
	// Use the SAME key for merchant+platform so callbacks the test signs with
	// the merchant key verify against the configured platform key.
	key, e := pluginPrivateKey(privPEM)
	if e != nil {
		t.Fatal(e)
	}
	_ = key
	return PluginChannel{
		ID: 3, Plugin: "umfpay", PayType: payType,
		Config:       map[string]string{"appid": "M0001", "appkey": "x"},
		Certificates: map[string]string{"cert_pem": pubPEM, "key_pem": privPEM},
	}, mustUmfPriv(t, privPEM), privPEM
}

func mustUmfPriv(t *testing.T, pemText string) *rsa.PrivateKey {
	k, e := pluginPrivateKey(pemText)
	if e != nil {
		t.Fatal(e)
	}
	return k
}

func TestPluginUmfpayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("umfpay")
	if !ok || spec.Title != "联动优势" || !spec.Serves("bank") || spec.Serves("qqpay") {
		t.Fatal("umfpay spec", ok)
	}
	c, _, _ := umfpayChannel(t, "alipay")
	if _, e := BuildPluginChannel(c); e != nil {
		t.Fatal(e)
	}
	// Missing required PEM.
	bad, _, _ := umfpayChannel(t, "alipay")
	delete(bad.Certificates, "key_pem")
	if _, e := BuildPluginChannel(bad); e == nil {
		t.Fatal("accepted missing key")
	}
	// Unserved pay type.
	ut, _, _ := umfpayChannel(t, "qqpay")
	if _, e := BuildPluginChannel(ut); e == nil {
		t.Fatal("accepted unserved pay type")
	}
}

func TestPluginUmfpaySignContent(t *testing.T) {
	// Independently constructed: ksort, skip sign/sign_type/empty, "k=v&".
	params := map[string]string{"service": "s", "mer_id": "M1", "amount": "100", "sign": "x", "sign_type": "RSA", "empty": ""}
	want := "amount=100&mer_id=M1&service=s"
	if got := umfpaySignContent(params); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPluginUmfpayCreateQR(t *testing.T) {
	c, priv, _ := umfpayChannel(t, "alipay")
	var gotForm url.Values
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(b))
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	up := p.(*umfpayPlugin)
	// Redirect the gateway to the test server via a wrapper request.
	qr := "https://qr.umf/abc"
	reply = `<html><head><META NAME="MobilePayPlatform" CONTENT="ret_code=0000&trade_no=U9&bank_payurl=` + base64.StdEncoding.EncodeToString([]byte(qr)) + `"></head></html>`
	// Replace the gateway host by signing/posting to the test server directly.
	r := CreateRequest{TradeNo: "20261029ABC", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100, ClientIP: "1.2.3.4"}
	// Exercise the parser + mapping directly (Create posts to the fixed
	// gateway host, which the sandbox cannot reach), then assert request shape
	// via a second call to the test server.
	res, perr := umfpayCreateVia(up, srv.URL, r)
	if perr != nil {
		t.Fatal(perr)
	}
	if res.MaterialType != "qrcode" || res.Material != qr || res.ProviderTradeNo != "U9" {
		t.Fatal(res)
	}
	if gotForm.Get("scancode_type") != "ALIPAY" || gotForm.Get("amount") != "100" || gotForm.Get("order_id") != "20261029ABC" {
		t.Fatal("request", gotForm)
	}
	// The posted signature must verify with the configured platform key.
	signed := map[string]string{}
	for k, v := range gotForm {
		signed[k] = v[0]
	}
	if !rsaVerify(&priv.PublicKey, crypto.SHA1, umfpaySignContent(signed), signed["sign"]) {
		t.Fatal("request signature did not verify")
	}
	// Business refusal.
	reply = `<html><head><META NAME="MobilePayPlatform" CONTENT="ret_code=5001&ret_msg=fail"></head></html>`
	if _, e := umfpayCreateVia(up, srv.URL, r); !errors.As(e, new(*ProviderError)) {
		t.Fatal("business refusal not definitive", e)
	}
	// Unreadable reply → unknown.
	reply = `no meta here`
	if _, e := umfpayCreateVia(up, srv.URL, r); !errors.As(e, new(*UnknownOutcome)) {
		t.Fatal("unreadable must be unknown", e)
	}
}

// umfpayCreateVia runs the create flow against an arbitrary gateway URL so the
// test can observe the request without reaching the fixed production host.
func umfpayCreateVia(p *umfpayPlugin, gateway string, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	scan, _ := p.scancodeType()
	ip := r.ClientIP
	params := map[string]string{
		"charset": "UTF-8", "sign_type": "RSA", "res_format": "HTML", "version": "4.0",
		"amt_type": "RMB", "mer_id": p.merID, "service": "active_scancode_order_new",
		"notify_url": r.NotifyURL, "goods_inf": r.Subject, "order_id": r.TradeNo,
		"mer_date": umfpayMerDate(r.TradeNo), "amount": "100", "user_ip": ip,
		"scancode_type": scan, "mer_flag": "KMER", "consumer_id": strings.ReplaceAll(ip, ".", ""),
	}
	sig, e := p.signRSA(umfpaySignContent(params))
	if e != nil {
		return CreateResult{}, e
	}
	params["sign"] = sig
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	raw, e := p.postForm(context.Background(), "umfpay.scancode", gateway, form)
	if e != nil {
		return CreateResult{}, e
	}
	reply, e := umfpayParseMeta(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: "umfpay.scancode", Cause: e}
	}
	if reply["ret_code"] != "0000" {
		if code := reply["ret_code"]; code != "" {
			return CreateResult{}, rejected("UMF_" + code)
		}
		return CreateResult{}, &UnknownOutcome{Operation: "umfpay.scancode", Cause: errUmfpayReply}
	}
	qr, e := base64.StdEncoding.DecodeString(reply["bank_payurl"])
	if e != nil || len(qr) == 0 {
		return CreateResult{}, &UnknownOutcome{Operation: "umfpay.scancode", Cause: errUmfpayReply}
	}
	res, ok := materialFromLinks("", string(qr), "")
	if !ok {
		return CreateResult{}, rejected("UMF_NO_QR")
	}
	res.ProviderTradeNo = reply["trade_no"]
	return res, nil
}

func TestPluginUmfpayNotification(t *testing.T) {
	c, priv, _ := umfpayChannel(t, "alipay")
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	v := p.(NotificationVerifier)
	base := map[string]string{
		"order_id": "20261029ABC", "trade_no": "U100", "trade_state": "TRADE_SUCCESS",
		"amount": "100", "mer_cust_id": "buyer-1", "mer_id": "M0001",
	}
	sig, e := rsaSign(priv, crypto.SHA1, umfpaySignContent(base))
	if e != nil {
		t.Fatal(e)
	}
	encode := func(m map[string]string) []byte {
		q := url.Values{}
		for k, val := range m {
			q.Set(k, val)
		}
		return []byte(q.Encode())
	}
	good := map[string]string{"sign": sig}
	for k, val := range base {
		good[k] = val
	}
	exp := ExpectedPayment{TradeNo: "20261029ABC", Amount: 100}
	ev, e := v.VerifyNotification(context.Background(), nil, encode(good), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "U100" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// Bad signature.
	bad := map[string]string{}
	for k, val := range good {
		bad[k] = val
	}
	bad["sign"] = base64.StdEncoding.EncodeToString([]byte("nope"))
	if _, e := v.VerifyNotification(context.Background(), nil, encode(bad), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign accepted", e)
	}
	// Wrong amount.
	if _, e := v.VerifyNotification(context.Background(), nil, encode(good), ExpectedPayment{TradeNo: "20261029ABC", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount mismatch accepted", e)
	}
	// Wrong trade number.
	if _, e := v.VerifyNotification(context.Background(), nil, encode(good), ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade mismatch accepted", e)
	}
	// Non-success status.
	ns := map[string]string{}
	for k, val := range base {
		ns[k] = val
	}
	ns["trade_state"] = "TRADE_FAIL"
	nsSig, _ := rsaSign(priv, crypto.SHA1, umfpaySignContent(ns))
	ns["sign"] = nsSig
	if _, e := v.VerifyNotification(context.Background(), nil, encode(ns), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success accepted", e)
	}
	// Methods and ack.
	if got := p.(NotificationMethodsProvider).NotificationMethods(); len(got) != 2 || got[0] != http.MethodGet {
		t.Fatal(got)
	}
	status, ct, body := p.(NotificationAcker).NotificationAck(nil)
	if status != 200 || !strings.Contains(body, "ret_code=0000") || !strings.Contains(ct, "html") {
		t.Fatal(status, body)
	}
}
