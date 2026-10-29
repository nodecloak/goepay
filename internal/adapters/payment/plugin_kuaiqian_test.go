package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// kuaiqianTestPlugin builds a plugin with in-memory RSA keys, bypassing PFX
// decoding (which needs a legacy PKCS#12 fixture that x/crypto/pkcs12 can read
// and no Encode exists to generate one in-process). The sign/verify logic is
// identical; only the key source differs.
func kuaiqianTestPlugin(t *testing.T, payType string) (*kuaiqianPlugin, *rsa.PrivateKey) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	base, e := newPluginBase(PluginChannel{Plugin: "kuaiqian", PayType: payType}, pluginAccount("https://www.99bill.com", "9999"))
	if e != nil {
		t.Fatal(e)
	}
	return &kuaiqianPlugin{pluginBase: base, appid: "9999", privateKey: key, publicKey: &key.PublicKey}, key
}

func TestPluginKuaiqianRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("kuaiqian")
	if !ok || spec.Title != "快钱支付" || !spec.Serves("alipay") || !spec.Serves("wxpay") || !spec.Serves("bank") || spec.Serves("qqpay") {
		t.Fatal("kuaiqian spec", spec, ok)
	}
	// Build must reject missing required inputs / bad fields offline. A channel
	// with no certificates fails the required-PEM check in BuildPluginChannel.
	base := PluginChannel{ID: 1, Plugin: "kuaiqian", PayType: "alipay", Config: map[string]string{"appid": "9999", "appkey": "pw"}}
	if _, e := BuildPluginChannel(base); e == nil {
		t.Fatal("accepted channel with no certs")
	}
	// Garbage PFX text is rejected.
	withCerts := base
	withCerts.Certificates = map[string]string{"merchant_pfx": "not-base64-!!", "platform_cert": "garbage"}
	if _, e := BuildPluginChannel(withCerts); e == nil {
		t.Fatal("accepted garbage pfx")
	}
	// Unknown pay type.
	bad := base
	bad.PayType = "qqpay"
	if _, e := BuildPluginChannel(bad); e == nil {
		t.Fatal("accepted unserved pay type")
	}
}

func TestPluginKuaiqianSignString(t *testing.T) {
	p, key := kuaiqianTestPlugin(t, "alipay")
	order := []string{"inputCharset", "pageUrl", "bgUrl", "version", "language", "signType", "merchantAcctId", "orderId", "orderAmount", "orderTime", "productName", "payType", "extDataType", "extDataContent"}
	params := map[string]string{
		"inputCharset":   "1",
		"pageUrl":        "https://pay.example/r",
		"bgUrl":          "https://pay.example/n",
		"version":        "mobile1.0",
		"language":       "1",
		"signType":       "4",
		"merchantAcctId": "999901",
		"orderId":        "T1",
		"orderAmount":    "100",
		"orderTime":      "19700101000000",
		"productName":    "VIP",
		"payType":        "27-3",
		"extDataType":    "", // empty -> skipped
	}
	// Build the expected string by hand: insertion order, skip empties.
	want := "inputCharset=1&pageUrl=https://pay.example/r&bgUrl=https://pay.example/n&version=mobile1.0&language=1&signType=4&merchantAcctId=999901&orderId=T1&orderAmount=100&orderTime=19700101000000&productName=VIP&payType=27-3"
	sig, e := p.generateSign(order, params)
	if e != nil {
		t.Fatal(e)
	}
	// Independently verify the signature is over exactly `want` with SHA256.
	if !rsaVerify(&key.PublicKey, crypto.SHA256, want, sig) {
		t.Fatal("signature not over expected string")
	}
}

func TestPluginKuaiqianCreateForm(t *testing.T) {
	p, _ := kuaiqianTestPlugin(t, "wxpay")
	// Pin the clock so orderTime (live Asia/Shanghai date('YmdHis')) is
	// deterministic; restore after the test.
	saved := kuaiqianNow
	kuaiqianNow = func() time.Time { return time.Date(2026, 10, 29, 9, 30, 15, 0, time.UTC) }
	defer func() { kuaiqianNow = saved }()

	if got := p.Capabilities().Methods; len(got) != 1 || got[0] != "h5" {
		t.Fatal(got)
	}
	r := CreateRequest{TradeNo: "20261029000001", Subject: `a"<b>`, Method: "h5", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", ClientIP: "1.2.3.4", Amount: 1000}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "html" {
		t.Fatal(res, e)
	}
	// Form posts to the mobile gateway, escapes the subject, carries wxpay's
	// payType 26-2 and the fen amount 1000 (cents == fen).
	if !strings.Contains(res.Material, `action="`+kuaiqianMobileGateway+`"`) || strings.Contains(res.Material, "<b>") || !strings.Contains(res.Material, `value="1000"`) || !strings.Contains(res.Material, `name="signMsg"`) || !strings.Contains(res.Material, `value="26-2"`) {
		t.Fatal(res.Material)
	}
	// orderId is the trade number; orderTime is the pinned clock in +08:00
	// (2026-10-29 09:30:15 UTC = 17:30:15 CST).
	if !strings.Contains(res.Material, `name="orderId" value="20261029000001"`) || !strings.Contains(res.Material, `name="orderTime" value="20261029173015"`) {
		t.Fatal("orderId/orderTime", res.Material)
	}
	// Unserved method rejected.
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "x", Method: "native", NotifyURL: r.NotifyURL, Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("native must be rejected", e)
	}
}

func TestPluginKuaiqianNotification(t *testing.T) {
	p, key := kuaiqianTestPlugin(t, "alipay")
	base := map[string]string{
		"merchantAcctId": "999901",
		"version":        "mobile1.0",
		"signType":       "4",
		"payType":        "27-3",
		"orderId":        "T1",
		"orderTime":      "20261029000000",
		"orderAmount":    "100",
		"dealId":         "D9",
		"payResult":      "10",
	}
	sign := func(m map[string]string) string {
		var sb strings.Builder
		for _, k := range kuaiqianVerifyOrder {
			if v := m[k]; v != "" {
				sb.WriteString(k + "=" + v + "&")
			}
		}
		msg := strings.TrimSuffix(sb.String(), "&")
		s, e := rsaSign(key, crypto.SHA256, msg)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	encode := func(m map[string]string) []byte {
		q := url.Values{}
		for k, v := range m {
			q.Set(k, v)
		}
		return []byte(q.Encode())
	}
	good := map[string]string{}
	for k, v := range base {
		good[k] = v
	}
	good["signMsg"] = sign(base)
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := p.VerifyNotification(context.Background(), nil, encode(good), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "D9" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// bad sign
	bad := map[string]string{}
	for k, v := range good {
		bad[k] = v
	}
	bad["signMsg"] = "AAAA"
	if _, e = p.VerifyNotification(context.Background(), nil, encode(bad), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// wrong amount (re-sign with a different amount so the signature matches but
	// the amount check fails)
	wrongAmt := map[string]string{}
	for k, v := range base {
		wrongAmt[k] = v
	}
	wrongAmt["orderAmount"] = "200"
	wrongAmt["signMsg"] = sign(wrongAmt)
	if _, e = p.VerifyNotification(context.Background(), nil, encode(wrongAmt), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	// wrong trade number
	if _, e = p.VerifyNotification(context.Background(), nil, encode(good), ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// non-success payResult
	ns := map[string]string{}
	for k, v := range base {
		ns[k] = v
	}
	ns["payResult"] = "11"
	ns["signMsg"] = sign(ns)
	if _, e = p.VerifyNotification(context.Background(), nil, encode(ns), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("status", e)
	}
	// ack bodies
	if status, ct, body := p.NotificationAck(nil); status != 200 || !strings.Contains(ct, "html") || body != "<result>1</result>" {
		t.Fatal(status, body)
	}
	if _, _, body := p.NotificationAck(ErrInvalid); body != "<result>0</result>" {
		t.Fatal(body)
	}
	if got := p.NotificationMethods(); len(got) != 2 || got[0] != http.MethodGet {
		t.Fatal(got)
	}
}
