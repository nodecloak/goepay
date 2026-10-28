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

func yseqtTestPlugin(t *testing.T, payType, gateway string) (*yseqtPlugin, *rsa.PrivateKey) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	base, e := newPluginBase(PluginChannel{Plugin: "yseqt", PayType: payType, HTTPClient: &http.Client{}}, "host|SRC1|PAYEE1")
	if e != nil {
		t.Fatal(e)
	}
	return &yseqtPlugin{pluginBase: base, srcMerc: "SRC1", payeeNo: "PAYEE1", priv: key, pub: &key.PublicKey, gateway: gateway}, key
}

func TestPluginYseqtRegistryAndScope(t *testing.T) {
	spec, ok := LookupPlugin("yseqt")
	if !ok || spec.Title != "银盛e企通" || !spec.Serves("bank") {
		t.Fatal("yseqt spec", ok)
	}
	// Transfers are out of scope: no transfer types declared.
	if len(spec.TransferTypes) != 0 {
		t.Fatal("transfer types should be empty", spec.TransferTypes)
	}
	// Bad PFX rejected.
	c := PluginChannel{ID: 1, Plugin: "yseqt", PayType: "wxpay",
		Config:       map[string]string{"appid": "SRC1", "appkey": "pwd", "appmchid": "PAYEE1"},
		Certificates: map[string]string{"appcert": "not-base64!!"}}
	if _, e := BuildPluginChannel(c); e == nil {
		t.Fatal("accepted bad pfx")
	}
	if _, e := yseqtBundledPublic(); e != nil {
		t.Fatal("bundled cert parse", e)
	}
}

func TestPluginYseqtSignContent(t *testing.T) {
	params := map[string]string{"serviceNo": "scanPay", "srcMerchantNo": "SRC1", "bizReqJson": `{"a":1}`, "sign": "x"}
	want := `bizReqJson={"a":1}&serviceNo=scanPay&srcMerchantNo=SRC1`
	if got := yseqtSignContent(params); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPluginYseqtCreate(t *testing.T) {
	var gotForm url.Values
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(b))
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, key := yseqtTestPlugin(t, "bank", srv.URL)

	// native scanPay → qrCode.
	reply = `{"code":"SYS000","bizResponseJson":"{\"subCode\":\"COM000\",\"qrCode\":\"https://qr.eqt/1\",\"tradeSn\":\"E1\"}"}`
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.eqt/1" || res.ProviderTradeNo != "E1" {
		t.Fatal(res, e)
	}
	if gotForm.Get("serviceNo") != "scanPay" || !strings.Contains(gotForm.Get("bizReqJson"), `"bankType":"9001002"`) {
		t.Fatal("request", gotForm)
	}
	signed := map[string]string{}
	for k, v := range gotForm {
		signed[k] = v[0]
	}
	if !rsaVerify(&key.PublicKey, crypto.SHA1, yseqtSignContent(signed), signed["sign"]) {
		t.Fatal("request signature did not verify")
	}

	// jsapi needs OpenID.
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "s", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi without openid accepted", e)
	}
	// bank jsapi → redirectUrl jump.
	reply = `{"code":"SYS000","bizResponseJson":"{\"subCode\":\"COM004\",\"tradeSn\":\"E2\",\"payData\":\"{\\\"redirectUrl\\\":\\\"https://u/x\\\"}\"}"}`
	res, e = p.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "s", Method: "jsapi", OpenID: "u-1", NotifyURL: "https://pay.example/n", Amount: 1})
	if e != nil || res.MaterialType != "jump" || res.Material != "https://u/x" {
		t.Fatal(res, e)
	}

	// Business refusal (subCode other).
	reply = `{"code":"SYS000","bizResponseJson":"{\"subCode\":\"COM999\",\"subMsg\":\"x\"}"}`
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T3", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.As(e, new(*ProviderError)) {
		t.Fatal("refusal not definitive", e)
	}
	// Transport failure code → unknown.
	reply = `{"code":"SYSERR","msg":"x"}`
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T4", Subject: "s", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.As(e, new(*UnknownOutcome)) {
		t.Fatal("transport error must be unknown", e)
	}
}

func TestPluginYseqtNotification(t *testing.T) {
	p, key := yseqtTestPlugin(t, "wxpay", "https://unused/")
	biz := `{"requestNo":"T9","tradeSn":"E9","openId":"o-9","amount":"1.00","state":"SUCCESS"}`
	base := map[string]string{"bizResponseJson": biz, "serviceNo": "payNotify", "srcMerchantNo": "SRC1"}
	sig, e := rsaSign(key, crypto.SHA1, yseqtSignContent(base))
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
	exp := ExpectedPayment{TradeNo: "T9", Amount: 100}
	ev, e := p.VerifyNotification(context.Background(), nil, encode(good), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "E9" || ev.Amount != 100 || ev.BuyerID != "o-9" {
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
	if _, e := p.VerifyNotification(context.Background(), nil, encode(good), ExpectedPayment{TradeNo: "T9", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount accepted", e)
	}
	if _, e := p.VerifyNotification(context.Background(), nil, encode(good), ExpectedPayment{TradeNo: "X", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade accepted", e)
	}
	// Non-success state.
	biz2 := `{"requestNo":"T9","tradeSn":"E9","amount":"1.00","state":"FAIL"}`
	base2 := map[string]string{"bizResponseJson": biz2, "serviceNo": "payNotify", "srcMerchantNo": "SRC1"}
	sig2, _ := rsaSign(key, crypto.SHA1, yseqtSignContent(base2))
	good2 := map[string]string{"sign": sig2}
	for k, v := range base2 {
		good2[k] = v
	}
	if _, e := p.VerifyNotification(context.Background(), nil, encode(good2), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success accepted", e)
	}
	// Ack.
	if status, _, b := p.NotificationAck(nil); status != 200 || b != "success" {
		t.Fatal(status, b)
	}
}
