package payment

import (
	"context"
	"crypto"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Fixed 2048-bit RSA test keypair (generated with node crypto). priv is the
// merchant key used to sign requests, pub the platform key used to verify.
const swiftpassTestPriv = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDD9e9jFtr0hNqZ
pPb954XRei8E8cD3ngQWorSX0+JH/YtVhJiScSYaJBs3q9VOg064rr3l4dcxTzNL
T585gFErjuSvbwd/JWLZkZn+C+sA4mh5Jsn0P/PKUitcJftDmNeeywdrgjTG+xGU
C2OSWTx2dnUm0lk5zedlllFL+E3ygSiwolbh2ybJm2VyJsiKC22Xj81xTvi79ZVY
Rb4TlvZ2JPNmntF7jYTx1dSvs1Pq1hhCUCJJeNvIZU+Ju5JE0FduNguOnqjAPyIl
m4X8QPZ1T9PrgYUbnp5AUk09zkyeEnWd2fgSYEXdQ21PYA/cYP5KN3SGn39amJvK
iQXB58l1AgMBAAECggEAB3cZCuIArrauSpLO7gFMnHGr1OO41uC9hcyRAK+2ZnOx
mS0XUDeLR2JUvxwxYMT+fL6mHASB2YO2Ooo/+0V40iX8GaArMvF5AJIxbEzb7l6x
stvYGbv4okkwaa7wvDSDEljoYXTGZJuFTJSFKJAfDjbuY5o5LUs7eNKZ1Ou9XHL0
vpjxVCO3oevsPfhpvqgkLy82VTakw+DLJSpkR8jTYLgM+XPpzGYFOovKB8X9ygR+
IWWq28LEr/UI9QIWuTBt0KOSGGoMsZcl/Kk5epK08xJMmiLImVgxIC7Iq/P5XfNT
AkJGz7bRe8BwBD0JBSqFB0dhRQ7RZiiGuffsUAUASQKBgQDwnSb2UDe/HDbQASQV
xnu1gzGfhFouFpyQ1h0ANt68pBBwXZ5SanLJ9kZ7ZeC9mr+rWeZA0AXP8qUQcR1M
qopHwV3A6bWG4fMw5shbPwqKy5aMzk5hamEgdKT1XvUXgsv4xc9jfhPGcWZVX9Iz
WWeQPMUhQu1KrQS3pO7gbzzOCwKBgQDQfc93BBlG+JBQ47dULwgUS4ayXAGCIhPf
DXOU91WI45HpWW+5EhR6zhDlZfvaDfTqOBI4R3MjEhIunhUJdBSJGxnFfBYRBVhb
xDQVgCZnyhDPK3suetCMl0GGErvzoKOPnd+tvGT716wJTmUyNi/1oHE+XtM+fWSi
MwCEWJ32fwKBgQCIQ8e7+OQJRDY6iFriRhzrR9hDiZX0wfN7Bvc3ltt/o753lF1E
OWq2u9VgruqfFlNz/B6tF+IiyTz32ceJEo/rYTPyO6kFMVMjn59aJZKX1gJ1zaSQ
AMnusoB1QYi1/un7AjOYGd+jI7ebo/+L+xbulaviUV/HScMBKdPqoLSjzwKBgQC0
5nS8r7G/Aq01gl/vi19XPLEC0RH999OGFgXTm0d8Hvjbn95wll9urp26bWK5MKLx
FvW4H0anh2Z/TGq0aSPoUrtpvuDmo9ymwvbuMOMh6xHqr6+SBL4vPo575oVuPlOs
qS8LcAuu+NMsv/jOOHYkibG4jE5+Sg6nLdmk+78TwQKBgEx8iSoYJTIDEGM9wGYw
3RJyQDBFVuhFpI47iSS1MiGtO6CM7cUfRcdGD6VP7wEfhGvX1krqyTDij+FU6qoK
aWebsdNLBmaGFD/wFV4qatfRkZn63JDkrPkd2wi40MKewRcRBHzPrDGuL4MnndfJ
2fWMQUnmgZUcDwSVHaST/NSx
-----END PRIVATE KEY-----`

const swiftpassTestPub = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAw/XvYxba9ITamaT2/eeF
0XovBPHA954EFqK0l9PiR/2LVYSYknEmGiQbN6vVToNOuK695eHXMU8zS0+fOYBR
K47kr28HfyVi2ZGZ/gvrAOJoeSbJ9D/zylIrXCX7Q5jXnssHa4I0xvsRlAtjklk8
dnZ1JtJZOc3nZZZRS/hN8oEosKJW4dsmyZtlcibIigttl4/NcU74u/WVWEW+E5b2
diTzZp7Re42E8dXUr7NT6tYYQlAiSXjbyGVPibuSRNBXbjYLjp6owD8iJZuF/ED2
dU/T64GFG56eQFJNPc5MnhJ1ndn4EmBF3UNtT2AP3GD+Sjd0hp9/WpibyokFwefJ
dQIDAQAB
-----END PUBLIC KEY-----`

// swiftpassSigVector is node's RSA-SHA256 signature of the exact sign content
// "mch_id=100001&nonce_str=abc&out_trade_no=T1&service=pay.weixin.native&total_fee=100".
const swiftpassSigVector = "PTjN4XYmv3NAkGbLQHaKwA+xqR9tZZFkcwbpIvmjc7GZaGUkfdsxE7vzLqsFaOQa71fNQljrc9ROF8Hna/bQ40eUzx1p5wN+7zaSmuUU9jw3SN+5k3gPNWEXwnKIwqwLiG6TD1/qU2TbpZaubY5fCopeNKeCe867zjyfpzzZtvu8gdZyjAs3dMIIai42lKfagVPUuFX/Qr+duTBdOi23iJaJw2HnogOWM0uIScXFoGbo1TZ3G3V2NO6uenCRieStBQZMUC/s0msMiSkSASzFRj+RyVa9pvYDpxMChNUxZufn5FGVZlOGQdUwDlQxieAy+Lg+Q5NJOFbrNAXZ1F3+ww=="

func swiftpassChannel(payType, gateway string) PluginChannel {
	c := PluginChannel{ID: 20, Plugin: "swiftpass", PayType: payType,
		Config:       map[string]string{"appid": "100001"},
		Certificates: map[string]string{"appkey": swiftpassTestPub, "appsecret": swiftpassTestPriv},
	}
	if gateway != "" {
		c.Config["appurl"] = gateway
	}
	return c
}

func TestPluginSwiftpassRegistry(t *testing.T) {
	spec, ok := LookupPlugin("swiftpass")
	if !ok || spec.Title != "威富通RSA" || !spec.Serves("bank") || spec.Serves("ecny") {
		t.Fatal("spec", spec, ok)
	}
	if _, e := BuildPluginChannel(swiftpassChannel("alipay", "")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{
		swiftpassChannel("ecny", ""),           // type not served
		swiftpassChannel("alipay", "ftp://x/"), // bad scheme
	}
	missing := swiftpassChannel("alipay", "")
	delete(missing.Certificates, "appsecret")
	badkey := swiftpassChannel("alipay", "")
	badkey.Certificates["appsecret"] = "not-a-key"
	bad = append(bad, missing, badkey)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

// Independent signature vector: the node-produced RSA-SHA256 signature must
// verify against swiftpassSignContent of the same parameters, proving our sign
// base matches SwiftpassClient::getSignContent exactly.
func TestPluginSwiftpassSignVector(t *testing.T) {
	p, e := BuildPluginChannel(swiftpassChannel("wxpay", ""))
	if e != nil {
		t.Fatal(e)
	}
	sp := p.(*swiftpassPlugin)
	params := map[string]string{"mch_id": "100001", "nonce_str": "abc", "out_trade_no": "T1", "service": "pay.weixin.native", "total_fee": "100", "sign": "x"}
	content := swiftpassSignContent(params)
	if content != "mch_id=100001&nonce_str=abc&out_trade_no=T1&service=pay.weixin.native&total_fee=100" {
		t.Fatal("sign content", content)
	}
	if !rsaVerify(sp.publicKey, crypto.SHA256, content, swiftpassSigVector) {
		t.Fatal("node signature did not verify against our sign content")
	}
	// And our own signer round-trips through verify.
	mine, e := sp.sign(params)
	if e != nil || !rsaVerify(sp.publicKey, crypto.SHA256, content, mine) {
		t.Fatal("self sign", mine, e)
	}
}

func TestPluginSwiftpassCreateNative(t *testing.T) {
	var gotBody string
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, e := BuildPluginChannel(swiftpassChannel("alipay", srv.URL+"/"))
	if e != nil {
		t.Fatal(e)
	}
	sp := p.(*swiftpassPlugin)

	ok := map[string]string{"status": "0", "result_code": "0", "code_url": "https://qr.alipay.com/abc", "transaction_id": "AL9"}
	sig, _ := sp.sign(ok)
	ok["sign"] = sig
	reply = swiftpassToXML(ok)

	r := CreateRequest{TradeNo: "T100", Subject: "商品A", Method: "native", NotifyURL: "https://pay.example/n", ClientIP: "203.0.113.9", Amount: 250}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.alipay.com/abc" || res.ProviderTradeNo != "AL9" {
		t.Fatal(res, e)
	}
	if !strings.Contains(gotBody, "<service><![CDATA[pay.alipay.native]]></service>") || !strings.Contains(gotBody, "<total_fee>250</total_fee>") || !strings.Contains(gotBody, "<sign_type><![CDATA[RSA_1_256]]></sign_type>") {
		t.Fatal("request body", gotBody)
	}
}

func TestPluginSwiftpassCreateOutcomes(t *testing.T) {
	var reply string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(swiftpassChannel("wxpay", srv.URL+"/"))
	sp := p.(*swiftpassPlugin)
	r := CreateRequest{TradeNo: "T2", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 50}

	fail := map[string]string{"status": "0", "result_code": "1", "err_code": "SYSTEMERROR"}
	sig, _ := sp.sign(fail)
	fail["sign"] = sig
	status, reply = 200, swiftpassToXML(fail)
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
	// Unsigned 2xx and 5xx are unknown.
	var unknown *UnknownOutcome
	status, reply = 200, "<xml><status>0</status><result_code>0</result_code><code_url>x</code_url></xml>"
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("unsigned reply must be unknown", e)
	}
	status, reply = 503, "<xml></xml>"
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("5xx must be unknown", e)
	}
}

func TestPluginSwiftpassNotification(t *testing.T) {
	p, _ := BuildPluginChannel(swiftpassChannel("wxpay", ""))
	sp := p.(*swiftpassPlugin)
	good := map[string]string{"status": "0", "result_code": "0", "out_trade_no": "T777", "total_fee": "100", "transaction_id": "WX77", "openid": "o-abc"}
	sig, _ := sp.sign(good)
	good["sign"] = sig
	raw := []byte(swiftpassToXML(good))
	exp := ExpectedPayment{TradeNo: "T777", Amount: 100}

	ev, e := sp.VerifyNotification(context.Background(), nil, raw, exp)
	if e != nil || ev.State != "SUCCESS" || ev.Amount != 100 || ev.ProviderTradeNo != "WX77" {
		t.Fatal(ev, e)
	}
	bad := map[string]string{}
	for k, v := range good {
		bad[k] = v
	}
	bad["sign"] = "AAAA"
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(swiftpassToXML(bad)), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	if _, e := sp.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "T777", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e := sp.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	ns := map[string]string{"status": "0", "result_code": "1", "out_trade_no": "T777", "total_fee": "100"}
	s2, _ := sp.sign(ns)
	ns["sign"] = s2
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(swiftpassToXML(ns)), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success", e)
	}
	if st, _, body := sp.NotificationAck(nil); st != 200 || body != "success" {
		t.Fatal(st, body)
	}
	if _, _, body := sp.NotificationAck(ErrInvalid); body != "failure" {
		t.Fatal(body)
	}
}

func TestPluginSwiftpassRefund(t *testing.T) {
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(swiftpassChannel("wxpay", srv.URL+"/"))
	sp := p.(*swiftpassPlugin)
	ok := map[string]string{"status": "0", "result_code": "0", "refund_id": "RF1"}
	sig, _ := sp.sign(ok)
	ok["sign"] = sig
	reply = swiftpassToXML(ok)
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T9", ProviderTradeNo: "WX9", RefundNo: "R9", Amount: 30, TotalAmount: 100})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 30 || rr.ProviderRefundNo != "RF1" {
		t.Fatal(rr, e)
	}
}
