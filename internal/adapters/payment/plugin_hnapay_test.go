package payment

import (
	"context"
	"crypto"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Same fixed RSA-2048 keypair as the huifu vectors (independent node crypto).
const hnapayTestPriv = huifuTestPriv
const hnapayTestPub = huifuTestPub

func hnapayChannel(payType, mchid string) PluginChannel {
	return PluginChannel{ID: 93, Plugin: "hnapay", PayType: payType, Config: map[string]string{
		"appid": "M1", "appkey": hnapayTestPub, "appsecret": hnapayTestPriv, "appmchid": mchid,
	}}
}

func TestPluginHnapayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("hnapay")
	if !ok || spec.Title != "新生支付" || !spec.Serves("bank") || spec.Serves("ecny") {
		t.Fatal("hnapay spec", ok)
	}
	if _, e := BuildPluginChannel(hnapayChannel("alipay", "RPT1")); e != nil {
		t.Fatal(e)
	}
	bad := hnapayChannel("alipay", "")
	bad.Config["appsecret"] = "nonsense"
	if _, e := BuildPluginChannel(bad); e == nil {
		t.Fatal("accepted bad private key")
	}
	missing := hnapayChannel("alipay", "")
	delete(missing.Config, "appkey")
	if _, e := BuildPluginChannel(missing); e == nil {
		t.Fatal("accepted missing public key")
	}
}

func TestPluginHnapayScanSignatureMatchesOriginal(t *testing.T) {
	built, _ := BuildPluginChannel(hnapayChannel("alipay", ""))
	p := built.(*hnapayPlugin)
	params := map[string]string{
		"tranCode": "WS01", "version": "2.1", "merId": "M1", "submitTime": "20261029010101",
		"merOrderNum": "T1", "tranAmt": "100", "payType": "QRCODE_B2C", "orgCode": "ALIPAY",
		"notifyUrl": "https://pay.ex/n", "charset": "1", "signType": "1",
	}
	order := []string{"tranCode", "version", "merId", "submitTime", "merOrderNum", "tranAmt", "payType", "orgCode", "notifyUrl", "charset", "signType"}
	content, ok := hnapaySignContent(params, order)
	if !ok {
		t.Fatal("content")
	}
	got, e := p.signHex(content)
	if e != nil {
		t.Fatal(e)
	}
	const want = "6ac1e41a5a4a759677b8eeba1aa2a8f501265602b77e3972efe8cd2b08d20176f16e3561e6f642d8a4ac50bf198cd667a3770deb89f9e9fe57f67beb2e5b7e81f82f9aa3a274bbf95291af55ddb3f1b57f97d41b289a4022c54aa9c6545173c22c84245375e49fa5c10a8eb0d08e2a4c2c4b8ee415928890b2f6802da944fa278e73ef3f35c35ff588e63e7407b0691adeafe8e3ae7bd5d08f8d5f1973a56ac7ea38adaf0717d24ddcf9405ccde34c8e9af095747a6911fc887befabd881883756b530329cb5169600e03a7e4818d235b18fc7795b57fc790a5b66bdb4345f54a6401d531630cf2de5bc4e29838c131d695f4af14da275abe1e85ee9c5c1e187"
	if got != want {
		t.Fatalf("hnapay scan sign mismatch\n got=%s", got)
	}
}

func TestPluginHnapayCreateScan(t *testing.T) {
	var reply string
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(b))
		if r.URL.Path != "/website/scanPay.do" {
			w.WriteHeader(404)
			return
		}
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	built, _ := BuildPluginChannel(hnapayChannel("alipay", ""))
	p := built.(*hnapayPlugin)
	p.gatewayBase = srv.URL

	// No signMsg in the reply -> the plugin skips the (old-key) reply verify.
	reply = `{"resultCode":"0000","hnapayOrderId":"H1","qrCodeUrl":"https://bank.ex/pay?qrContent=https%3A%2F%2Fqr.ex%2FX1&sign=zzz"}`
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "20261029010101T", Subject: "商品", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100, ClientIP: "1.2.3.4"})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.ex/X1" || res.ProviderTradeNo != "H1" {
		t.Fatal(res, e)
	}
	if form.Get("tranCode") != "WS01" || form.Get("tranAmt") != "100" || form.Get("signMsg") == "" || form.Get("payType") != "QRCODE_B2C" {
		t.Fatal("request", form)
	}
	// definitive refusal
	reply = `{"resultCode":"9001","msgExt":"通道维护"}`
	var pe *ProviderError
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "20261029010101T", Subject: "x", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100}); !errors.As(e, &pe) {
		t.Fatal("refusal", e)
	}
	// unknown outcome
	reply = `not json`
	var unk *UnknownOutcome
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "20261029010101T", Subject: "x", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100}); !errors.As(e, &unk) {
		t.Fatal("unknown", e)
	}
}

func TestPluginHnapayJSAPINeedsOpenID(t *testing.T) {
	built, _ := BuildPluginChannel(hnapayChannel("alipay", "RPT1"))
	p := built.(*hnapayPlugin)
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "20261029010101T", Subject: "x", Method: "jsapi", NotifyURL: "https://pay.ex/n", Amount: 100}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi needs openid", e)
	}
	// jsapi needs the报备编号 (appmchid).
	noMch, _ := BuildPluginChannel(hnapayChannel("alipay", ""))
	if _, e := noMch.Create(context.Background(), CreateRequest{TradeNo: "20261029010101T", Subject: "x", Method: "jsapi", NotifyURL: "https://pay.ex/n", Amount: 100, OpenID: "o-1"}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi needs mchid", e)
	}
}

func TestPluginHnapayJSAPINotification(t *testing.T) {
	built, _ := BuildPluginChannel(hnapayChannel("wxpay", "RPT1"))
	p := built.(*hnapayPlugin)
	v := built.(NotificationVerifier)
	// JSAPI callback: resultCode + base64 signValue over the new-key order.
	fields := map[string]string{
		"version": "2.0", "tranCode": "ITA10N", "merOrderId": "T1", "merId": "M1", "merAttach": "",
		"charset": "1", "signType": "1", "hnapayOrderId": "H1", "resultCode": "0000",
		"tranAmt": "1.00", "submitTime": "20261029010101", "tranFinishTime": "20261029010110", "userId": "u-9",
	}
	order := []string{"version", "tranCode", "merOrderId", "merId", "merAttach", "charset", "signType", "hnapayOrderId", "resultCode", "tranAmt", "submitTime", "tranFinishTime"}
	content, _ := hnapaySignContent(fields, order)
	k, _ := pluginPrivateKey(hnapayTestPriv)
	sig, _ := rsaSign(k, crypto.SHA1, content)
	fields["signValue"] = sig
	raw := hnapayForm(fields).Encode()
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := v.VerifyNotification(context.Background(), nil, []byte(raw), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "H1" || ev.BuyerID != "u-9" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// tamper sign
	bad := map[string]string{}
	for k2, v2 := range fields {
		bad[k2] = v2
	}
	bad["signValue"] = strings.Repeat("A", 344)
	if _, e = v.VerifyNotification(context.Background(), nil, []byte(hnapayForm(bad).Encode()), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// wrong amount
	if _, e = v.VerifyNotification(context.Background(), nil, []byte(raw), ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	// wrong trade
	if _, e = v.VerifyNotification(context.Background(), nil, []byte(raw), ExpectedPayment{TradeNo: "X", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// non-success status (re-sign with resultCode 9999)
	ns := map[string]string{}
	for k2, v2 := range fields {
		ns[k2] = v2
	}
	ns["resultCode"] = "9999"
	nsContent, _ := hnapaySignContent(ns, order)
	nsSig, _ := rsaSign(k, crypto.SHA1, nsContent)
	ns["signValue"] = nsSig
	if _, e = v.VerifyNotification(context.Background(), nil, []byte(hnapayForm(ns).Encode()), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non success", e)
	}
	if status, _, body := p.NotificationAck(nil); status != 200 || body != "200" {
		t.Fatal(status, body)
	}
	if _, _, body := p.NotificationAck(ErrInvalid); body != "sign_error" {
		t.Fatal("fail ack", body)
	}
}
