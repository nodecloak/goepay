package payment

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func zyuChannel(api, sw, payType string) PluginChannel {
	cfg := map[string]string{"appurl": api, "appid": "1000", "appkey": "synthetic-key", "appmchid": "BC1"}
	if sw != "" {
		cfg["appswitch"] = sw
	}
	return PluginChannel{ID: 14, Plugin: "zyu", PayType: payType, Config: cfg}
}

func TestPluginZyuRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("zyu")
	if !ok || spec.Title != "知宇支付" || !spec.Serves("bank") || spec.Serves("jdpay") {
		t.Fatal("spec", ok)
	}
	if _, e := BuildPluginChannel(zyuChannel("https://gw.example/pay", "0", "alipay")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{
		zyuChannel("https://gw.example/pay", "0", "jdpay"), // type not served
		zyuChannel("ftp://gw.example/", "0", "alipay"),     // scheme
	}
	for _, miss := range []string{"appurl", "appid", "appkey", "appmchid"} {
		c := zyuChannel("https://gw.example/pay", "0", "alipay")
		delete(c.Config, miss)
		bad = append(bad, c)
	}
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad", i)
		}
	}
}

func TestPluginZyuPageModeForm(t *testing.T) {
	defer func(f func() time.Time) { zyuNow = f }(zyuNow)
	zyuNow = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	p, _ := BuildPluginChannel(zyuChannel("https://gw.example/pay", "0", "alipay"))
	if got := p.Capabilities().Methods; len(got) != 1 || got[0] != "page" {
		t.Fatal(got)
	}
	r := CreateRequest{TradeNo: "T1", Subject: `a"<b>`, Method: "page", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 123}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "html" || !strings.Contains(res.Material, `action="https://gw.example/pay"`) || strings.Contains(res.Material, "<b>") || !strings.Contains(res.Material, `name="pay_md5sign"`) {
		t.Fatal(res, e)
	}
	// The embedded sign matches the independently computed vector.
	if !strings.Contains(res.Material, `value="7EFE22B41DCC09A3F850B7F9A341D405"`) {
		t.Fatal("sign not in form:", res.Material)
	}
}

func TestPluginZyuRequestModeQR(t *testing.T) {
	defer func(f func() time.Time) { zyuNow = f }(zyuNow)
	zyuNow = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var form url.Values
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(b))
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(zyuChannel(srv.URL, "2", "wxpay"))
	if got := p.Capabilities().Methods; len(got) != 1 || got[0] != "native" {
		t.Fatal(got)
	}
	reply = `{"status":200,"data":{"payUrl":"weixin://wxpay/x"}}`
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 123})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "weixin://wxpay/x" {
		t.Fatal(res, e)
	}
	if form.Get("pay_memberid") != "1000" || form.Get("pay_md5sign") == "" || form.Get("pay_productname") != "VIP" {
		t.Fatal("request", form)
	}
	reply = `{"status":500,"msg":"x"}`
	var pe *ProviderError
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 123}); !errors.As(e, &pe) {
		t.Fatal("refusal", e)
	}
	reply = `not json`
	var unknown *UnknownOutcome
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 123}); !errors.As(e, &unknown) {
		t.Fatal("unknown", e)
	}
}

func TestPluginZyuNotification(t *testing.T) {
	p, _ := BuildPluginChannel(zyuChannel("https://gw.example/pay", "0", "alipay"))
	v := p.(NotificationVerifier)
	base := map[string]string{"memberid": "1000", "orderid": "T1", "amount": "1.23", "datetime": "2026-01-01 00:00:00", "transaction_id": "TX1", "returncode": "00"}
	base["sign"] = "0481F8A276FF39AE7F99F62E66956428" // computed independently
	encode := func(m map[string]string) []byte {
		q := url.Values{}
		for k, val := range m {
			q.Set(k, val)
		}
		return []byte(q.Encode())
	}
	exp := ExpectedPayment{TradeNo: "T1", Amount: 123}
	ev, e := v.VerifyNotification(context.Background(), nil, encode(base), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "TX1" || ev.Amount != 123 {
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
	if _, e = v.VerifyNotification(context.Background(), nil, encode(tamper("amount", "9.99")), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount")
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(base), ExpectedPayment{TradeNo: "OTHER", Amount: 123}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade")
	}
	if status, _, body := p.(NotificationAcker).NotificationAck(nil); status != 200 || body != "OK" {
		t.Fatal(status, body)
	}
	if got := p.(NotificationMethodsProvider).NotificationMethods(); len(got) != 2 {
		t.Fatal(got)
	}
}
