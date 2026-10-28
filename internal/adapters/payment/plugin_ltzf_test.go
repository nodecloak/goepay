package payment

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func ltzfChannel(payType string, appTypes []string) PluginChannel {
	return PluginChannel{ID: 13, Plugin: "ltzf", PayType: payType, AppTypes: appTypes, Config: map[string]string{"appid": "1000", "appkey": "synthetic-key"}}
}

func TestPluginLtzfRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("ltzf")
	if !ok || spec.Title != "蓝兔支付" || !spec.Serves("wxpay") || spec.Serves("qqpay") {
		t.Fatal("spec", ok)
	}
	if _, e := BuildPluginChannel(ltzfChannel("wxpay", []string{"1", "3"})); e != nil {
		t.Fatal(e)
	}
	// alipay cannot use app type 3 (公众号).
	if _, e := BuildPluginChannel(ltzfChannel("alipay", []string{"3"})); e == nil {
		t.Fatal("alipay accepted apptype 3")
	}
	missing := ltzfChannel("wxpay", nil)
	delete(missing.Config, "appkey")
	if _, e := BuildPluginChannel(missing); e == nil {
		t.Fatal("accepted missing key")
	}
	if _, e := BuildPluginChannel(ltzfChannel("qqpay", nil)); e == nil {
		t.Fatal("accepted unserved type")
	}
}

func TestPluginLtzfMethods(t *testing.T) {
	p, _ := BuildPluginChannel(ltzfChannel("wxpay", []string{"1", "2", "3"}))
	m := p.Capabilities().Methods
	want := map[string]bool{"native": true, "h5": true, "jsapi": true}
	if len(m) != 3 {
		t.Fatal(m)
	}
	for _, x := range m {
		if !want[x] {
			t.Fatal(m)
		}
	}
	// Empty app types default to native.
	p2, _ := BuildPluginChannel(ltzfChannel("alipay", nil))
	if got := p2.Capabilities().Methods; len(got) != 1 || got[0] != "native" {
		t.Fatal(got)
	}
}

func TestPluginLtzfCreateAndSign(t *testing.T) {
	defer func(f func() int64) { ltzfNow = f }(ltzfNow)
	ltzfNow = func() int64 { return 1700000000 }
	var path string
	var form url.Values
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(b))
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(ltzfChannel("wxpay", []string{"1"}))
	lp := p.(*ltzfPlugin)
	lp.apiBase = srv.URL

	reply = `{"code":0,"data":{"code_url":"weixin://wxpay/bizpayurl?pr=abc"}}`
	res, e := lp.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 123})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "weixin://wxpay/bizpayurl?pr=abc" {
		t.Fatal(res, e)
	}
	if path != "/api/wxpay/native" || form.Get("sign") != "515B0AEEAD11859EF37E6FF70F41AB8C" || form.Get("total_fee") != "1.23" {
		t.Fatal("request", path, form.Get("sign"))
	}
	reply = `{"code":-1,"msg":"x"}`
	var pe *ProviderError
	if _, e = lp.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 123}); !errors.As(e, &pe) {
		t.Fatal("refusal", e)
	}
	reply = `not json`
	var unknown *UnknownOutcome
	if _, e = lp.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 123}); !errors.As(e, &unknown) {
		t.Fatal("unknown", e)
	}
}

func TestPluginLtzfNotification(t *testing.T) {
	p, _ := BuildPluginChannel(ltzfChannel("wxpay", []string{"1"}))
	v := p.(NotificationVerifier)
	base := map[string]string{"code": "0", "timestamp": "1700000000", "mch_id": "1000", "order_no": "O1", "out_trade_no": "T1", "pay_no": "P1", "total_fee": "1.23", "openid": "ox"}
	base["sign"] = "7C46D641892471D97398B37524845FFA" // computed independently
	encode := func(m map[string]string) []byte {
		q := url.Values{}
		for k, val := range m {
			q.Set(k, val)
		}
		return []byte(q.Encode())
	}
	exp := ExpectedPayment{TradeNo: "T1", Amount: 123}
	ev, e := v.VerifyNotification(context.Background(), nil, encode(base), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "O1" || ev.BuyerID != "ox" || ev.Amount != 123 {
		t.Fatal(ev, e)
	}
	tamper := func(k, val string) map[string]string {
		m := map[string]string{}
		for kk, vv := range base {
			m[kk] = vv
		}
		m[k] = val
		return m
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(tamper("sign", "00000000000000000000000000000000")), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign")
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(tamper("total_fee", "9.99")), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount")
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(base), ExpectedPayment{TradeNo: "OTHER", Amount: 123}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade")
	}
	if status, _, body := p.(NotificationAcker).NotificationAck(nil); status != 200 || body != "SUCCESS" {
		t.Fatal(status, body)
	}
	if _, _, body := p.(NotificationAcker).NotificationAck(ErrInvalid); body != "FAIL" {
		t.Fatal(body)
	}
}
