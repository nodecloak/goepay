package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Reuses adapayTestPriv/adapayTestPub as the merchant/platform RSA keys.
// The APIv3 symmetric key below is the exact 32-byte key used by scratch/
// vec_wxpaynp.js to build the encrypted notification ciphertext.
const wxpaynpAPIV3 = "anapiv3key0123456789abcdef012345"

func wxpaynpChannel(appTypes []string) PluginChannel {
	return PluginChannel{
		ID:       51,
		Plugin:   "wxpaynp",
		PayType:  "wxpay",
		AppTypes: appTypes,
		Config: map[string]string{
			"appid": "wx8888", "appmchid": "1900000109", "appsecret": wxpaynpAPIV3,
			"appkey": "SERIAL123", "publickeyid": "", "appurl": "1900000110", "appswitch": "0",
		},
		Certificates: map[string]string{
			"merchant_privatekey": adapayTestPriv,
			"platform_pubkey":     adapayTestPub,
		},
	}
}

func TestPluginWxpaynpRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("wxpaynp")
	if !ok || spec.Title != "微信官方支付V3服务商版" || !spec.Serves("wxpay") || spec.Serves("alipay") {
		t.Fatal("wxpaynp spec", spec, ok)
	}
	if _, e := BuildPluginChannel(wxpaynpChannel([]string{"1"})); e != nil {
		t.Fatal(e)
	}
	// Bad APIv3 key length.
	badKey := wxpaynpChannel([]string{"1"})
	badKey.Config["appsecret"] = "tooshort"
	// Missing merchant private key.
	missingPriv := wxpaynpChannel([]string{"1"})
	delete(missingPriv.Certificates, "merchant_privatekey")
	// Unknown literal sub_mchid key (would also trip channelconfig).
	unknown := wxpaynpChannel([]string{"1"})
	unknown.Config["sub_mchid"] = "x"
	for i, c := range []PluginChannel{badKey, missingPriv, unknown} {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginWxpaynpRequestSignature(t *testing.T) {
	// Independently computed by node RSA-SHA256 (scratch/vec_wxpaynp.js) over
	// METHOD\nURL\nts\nnonce\nbody\n.
	p, _ := BuildPluginChannel(wxpaynpChannel([]string{"1"}))
	wp := p.(*wxpaynpPlugin)
	body := wp.orderBody(CreateRequest{TradeNo: "T1", Subject: "商品", Amount: 100, NotifyURL: "https://pay.example/n", ClientIP: "192.0.2.1"})
	wantBody := `{"sp_appid":"wx8888","sp_mchid":"1900000109","sub_mchid":"1900000110","description":"商品","out_trade_no":"T1","notify_url":"https://pay.example/n","amount":{"total":100,"currency":"CNY"},"scene_info":{"payer_client_ip":"192.0.2.1"}}`
	if string(body) != wantBody {
		t.Fatalf("body mismatch:\n got=%s\nwant=%s", body, wantBody)
	}
	sig, e := SignWechatV3Request("POST", "/v3/pay/partner/transactions/native", "1700000000", "nonceabc", body, wp.privateKey)
	if e != nil {
		t.Fatal(e)
	}
	const want = "fducPA+Q3uXjZ/BEp7SSfKa7yFOcdTrSrp82O+1la1BoKFQdx66FR5DGdVXR3nK3TB1k1mEwZ4sdI7vBWxcDGGD9NDvbGYPamQxjqtYYYmeQ4CQS5b31TMG0s8cUaIj+rOUGs/L54cEgqdGLrt+q4SQ2TiCgrJLTW8ArT5udW3NGYJszwM0VhnGlPAhEaWAj4cEMJc7ENGxbH+NUVZui/tQwxV5aShhzcebrV0DIWJJ/QE5dqVt3hHn8VODBsIR/MFaWw0dRRTEFG0qvxZOkRg7DSQgn2DMAeeeo2PTI/XFyCXWc+KpYdiP/B7VFGlZMqJV2RUb8eFESQBTKg1xf9g=="
	if sig != want {
		t.Fatalf("wxpaynp v3 sign vector mismatch:\n got=%s\nwant=%s", sig, want)
	}
}

func TestPluginWxpaynpCreateNative(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	var reply string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(wxpaynpChannel([]string{"1"}))
	wp := p.(*wxpaynpPlugin)
	status = 200
	reply = `{"code_url":"weixin://wxpay/bizpayurl?pr=x"}`
	body := wp.orderBody(CreateRequest{TradeNo: "T1", Subject: "商品", Amount: 100, NotifyURL: "https://pay.example/n", ClientIP: "192.0.2.1"})
	// execute() targets the const gateway; drive a raw call against the server
	// using the same signing path via a dedicated helper call.
	raw, e := wp.executeAt(context.Background(), "op", http.MethodPost, srv.URL, "/v3/pay/partner/transactions/native", body)
	if e != nil {
		t.Fatal(e)
	}
	if gotPath != "/v3/pay/partner/transactions/native" || !strings.HasPrefix(gotAuth, "WECHATPAY2-SHA256-RSA2048 ") {
		t.Fatal("request", gotPath, gotAuth)
	}
	if !strings.Contains(gotBody, `"sub_mchid":"1900000110"`) || !strings.Contains(gotBody, `"sp_mchid":"1900000109"`) {
		t.Fatal("body fields", gotBody)
	}
	out, _ := callbackJSON(raw)
	if jsonText(out, "code_url") != "weixin://wxpay/bizpayurl?pr=x" {
		t.Fatal(out)
	}
	// 5xx -> unknown; a 4xx -> definitive.
	status, reply = 500, "x"
	var uo *UnknownOutcome
	if _, e := wp.executeAt(context.Background(), "op", http.MethodPost, srv.URL, "/v3/x", body); !errors.As(e, &uo) {
		t.Fatal("5xx not unknown", e)
	}
	status, reply = 403, "x"
	var pe *ProviderError
	if _, e := wp.executeAt(context.Background(), "op", http.MethodPost, srv.URL, "/v3/x", body); !errors.As(e, &pe) {
		t.Fatal("4xx not definitive", e)
	}
}

func TestPluginWxpaynpNotification(t *testing.T) {
	p, _ := BuildPluginChannel(wxpaynpChannel([]string{"1"}))
	wp := p.(*wxpaynpPlugin)

	// The AEAD ciphertext is produced independently by node (vec_wxpaynp.js):
	// plaintext = a SUCCESS transaction for T1 / 100 fen / openid oABC.
	const ciphertext = "f9/jtQRjykJdKHNeEYL8ESD1Cdf8fr2cjwCducpXfPGRr9P4xGLyAFL/Dcw4uPwXxzuCM/9GK9AuSPE9odRvHFSNQjvJCmjV1ami2P8Ej5rI0WPm+N4TmwOnbYYCwgZwmhAyHvZmRHSPB0qZEiJXEWygpyZWTsduYs24jAb9M/x+Xuaum7m21rkS786b1Fct7wdXoCfecRkWoG/W3ssAUDC8XNvfk6fO19Yzlll7e+f2we0X"
	cbBody := `{"id":"evt1","resource_type":"encrypt-resource","event_type":"TRANSACTION.SUCCESS","resource":{"algorithm":"AEAD_AES_256_GCM","ciphertext":"` + ciphertext + `","associated_data":"transaction","nonce":"abcdef012345"}}`

	// Sign the callback at runtime with a FRESH timestamp so the ±5min window
	// in VerifyWechatV3Message passes (the plugin uses time.Now()). The
	// signature here is RSA-SHA256 over ts\nnonce\nbody\n, computed in the test
	// (independent of the plugin's verification code).
	priv := wp.privateKey
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "cbnonce"
	msg := ts + "\n" + nonce + "\n" + cbBody + "\n"
	digest := sha256.Sum256([]byte(msg))
	sigBytes, e := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest[:])
	if e != nil {
		t.Fatal(e)
	}
	sig := base64.StdEncoding.EncodeToString(sigBytes)

	header := http.Header{}
	header.Set("Wechatpay-Timestamp", ts)
	header.Set("Wechatpay-Nonce", nonce)
	header.Set("Wechatpay-Serial", "SERIAL123")
	header.Set("Wechatpay-Signature", sig)

	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := wp.VerifyNotification(context.Background(), header, []byte(cbBody), exp)
	if e != nil || ev.State != "SUCCESS" || ev.Amount != 100 || ev.ProviderTradeNo != "42000" || ev.BuyerID != "oABC" {
		t.Fatal(ev, e)
	}
	// Wrong amount / trade no (signature still valid).
	if _, e := wp.VerifyNotification(context.Background(), header, []byte(cbBody), ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount")
	}
	if _, e := wp.VerifyNotification(context.Background(), header, []byte(cbBody), ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade")
	}
	// Tampered signature.
	bad := header.Clone()
	bad.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("0", 256))))
	if _, e := wp.VerifyNotification(context.Background(), bad, []byte(cbBody), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign accepted", e)
	}
	// Ack bodies.
	if st, ct, b := wp.NotificationAck(nil); st != 200 || !strings.Contains(b, "SUCCESS") || !strings.Contains(ct, "json") {
		t.Fatal("ack ok", st, b)
	}
	if st, _, b := wp.NotificationAck(errors.New("x")); st == 200 || !strings.Contains(b, "FAIL") {
		t.Fatal("ack fail", st, b)
	}
}
