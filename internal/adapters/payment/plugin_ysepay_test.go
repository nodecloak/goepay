package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// ysepayTestPlugin builds a plugin struct directly with a test RSA key as both
// the merchant private key and the platform public key (PFX decoding is covered
// by the Build-rejection test; here the key round-trips sign/verify).
func ysepayTestPlugin(t *testing.T, payType, gateway string) (*ysepayPlugin, *rsa.PrivateKey) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	base, e := newPluginBase(PluginChannel{Plugin: "ysepay", PayType: payType, HTTPClient: &http.Client{}}, "host|P1|S1|BC")
	if e != nil {
		t.Fatal(e)
	}
	return &ysepayPlugin{pluginBase: base, partnerID: "P1", sellerID: "S1", businessNo: "BC", priv: key, pub: &key.PublicKey, qrGateway: gateway}, key
}

func TestPluginYsepayRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("ysepay")
	if !ok || spec.Title != "银盛支付" || !spec.Serves("qqpay") || spec.Serves("jdpay") {
		t.Fatal("ysepay spec", ok)
	}
	// Build requires a decodable PFX; a non-base64 cert is rejected.
	c := PluginChannel{ID: 1, Plugin: "ysepay", PayType: "alipay",
		Config:       map[string]string{"appid": "P1", "appkey": "pwd", "appurl": "BIZ"},
		Certificates: map[string]string{"appcert": "not-base64!!"}}
	if _, e := BuildPluginChannel(c); e == nil {
		t.Fatal("accepted bad pfx")
	}
	// Missing required appurl.
	c2 := PluginChannel{ID: 1, Plugin: "ysepay", PayType: "alipay",
		Config:       map[string]string{"appid": "P1", "appkey": "pwd"},
		Certificates: map[string]string{"appcert": "AAAA"}}
	if _, e := BuildPluginChannel(c2); e == nil {
		t.Fatal("accepted missing business code")
	}
	// The bundled platform cert parses.
	if _, e := ysepayBundledPublic(); e != nil {
		t.Fatal("bundled cert parse", e)
	}
}

func TestPluginYsepaySignContent(t *testing.T) {
	params := map[string]string{"method": "m", "partner_id": "P1", "biz_content": `{"a":1}`, "sign": "x", "empty": " "}
	want := `biz_content={"a":1}&method=m&partner_id=P1`
	if got := ysepaySignContent(params); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPluginYsepayCreateQR(t *testing.T) {
	var gotForm url.Values
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(b))
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, key := ysepayTestPlugin(t, "alipay", srv.URL)

	// Build a signed success response: signData is the node object text.
	node := `{"code":"10000","trade_no":"U1","source_qr_code_url":"https://qr.yse/1"}`
	sig, e := rsaSign(key, crypto.SHA1, node)
	if e != nil {
		t.Fatal(e)
	}
	reply = `{"ysepay_online_qrcodepay_response":` + node + `,"sign":"` + sig + `"}`

	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "20261029T1", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100, ClientIP: "1.2.3.4"})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.yse/1" || res.ProviderTradeNo != "U1" {
		t.Fatal(res, e)
	}
	if gotForm.Get("method") != "ysepay.online.qrcodepay" || !strings.Contains(gotForm.Get("biz_content"), `"bank_type":"1903000"`) {
		t.Fatal("request", gotForm)
	}
	// The request signature must verify with the test public key.
	signed := map[string]string{}
	for k, v := range gotForm {
		signed[k] = v[0]
	}
	if !rsaVerify(&key.PublicKey, crypto.SHA1, ysepaySignContent(signed), signed["sign"]) {
		t.Fatal("request signature did not verify")
	}

	// wap mode renders an html form for alipay.
	wres, e := p.Create(context.Background(), CreateRequest{TradeNo: "20261029T2", Subject: `a"b`, Method: "wap", NotifyURL: "https://pay.example/n", Amount: 100})
	if e != nil || wres.MaterialType != "html" || !strings.Contains(wres.Material, "wap.directpay.createbyuser") {
		t.Fatal(wres, e)
	}

	// Business sub_code refusal.
	sub := `{"code":"40004","sub_code":"ACQ.X"}`
	ssig, _ := rsaSign(key, crypto.SHA1, sub)
	reply = `{"ysepay_online_qrcodepay_response":` + sub + `,"sign":"` + ssig + `"}`
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "20261029T3", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.As(e, new(*ProviderError)) {
		t.Fatal("refusal not definitive", e)
	}
	// Bad response signature → unknown.
	reply = `{"ysepay_online_qrcodepay_response":` + node + `,"sign":"AAAA"}`
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "20261029T4", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.As(e, new(*UnknownOutcome)) {
		t.Fatal("bad resp sign must be unknown", e)
	}
}

func TestPluginYsepayNotification(t *testing.T) {
	p, key := ysepayTestPlugin(t, "alipay", "https://unused/")
	base := map[string]string{
		"out_trade_no": "20261029T9", "trade_no": "U9", "buyer_user_id": "b-9",
		"total_amount": "1.00", "trade_status": "TRADE_SUCCESS",
	}
	sig, e := rsaSign(key, crypto.SHA1, ysepaySignContent(base))
	if e != nil {
		t.Fatal(e)
	}
	encode := func(m map[string]string) []byte {
		q := url.Values{}
		for k, v := range m {
			q.Set(k, v)
		}
		return []byte(q.Encode())
	}
	good := map[string]string{"sign": sig}
	for k, v := range base {
		good[k] = v
	}
	exp := ExpectedPayment{TradeNo: "20261029T9", Amount: 100}
	ev, e := p.VerifyNotification(context.Background(), nil, encode(good), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "U9" || ev.Amount != 100 || ev.BuyerID != "b-9" {
		t.Fatal(ev, e)
	}
	// Bad sign.
	bad := map[string]string{}
	for k, v := range good {
		bad[k] = v
	}
	bad["sign"] = "AAAA"
	if _, e := p.VerifyNotification(context.Background(), nil, encode(bad), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign accepted", e)
	}
	// Wrong amount / trade.
	if _, e := p.VerifyNotification(context.Background(), nil, encode(good), ExpectedPayment{TradeNo: "20261029T9", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount accepted", e)
	}
	if _, e := p.VerifyNotification(context.Background(), nil, encode(good), ExpectedPayment{TradeNo: "X", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade accepted", e)
	}
	// Non-success status.
	ns := map[string]string{}
	for k, v := range base {
		ns[k] = v
	}
	ns["trade_status"] = "WAIT_BUYER_PAY"
	nsSig, _ := rsaSign(key, crypto.SHA1, ysepaySignContent(ns))
	ns["sign"] = nsSig
	if _, e := p.VerifyNotification(context.Background(), nil, encode(ns), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success accepted", e)
	}
	// Ack.
	if status, _, b := p.NotificationAck(nil); status != 200 || b != "success" {
		t.Fatal(status, b)
	}
}
