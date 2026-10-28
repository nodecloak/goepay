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
)

func alipayhkChannel(appTypes []string) PluginChannel {
	return PluginChannel{ID: 9, Plugin: "alipayhk", PayType: "alipay",
		Config:   map[string]string{"appid": "2088000000000001", "appkey": "synthetic-md5-key", "appswitch": "0"},
		AppTypes: appTypes,
	}
}

func TestPluginAlipayhkRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("alipayhk")
	if !ok || spec.Title != "AlipayHK" || !spec.Serves("alipay") || len(spec.AppTypes) != 3 {
		t.Fatal("spec", spec, ok)
	}
	if _, e := BuildPluginChannel(alipayhkChannel([]string{"1"})); e != nil {
		t.Fatal(e)
	}
	// Unknown app type.
	badApp := alipayhkChannel([]string{"9"})
	// Missing key.
	missing := alipayhkChannel([]string{"1"})
	delete(missing.Config, "appkey")
	// Unserved pay type.
	unserved := alipayhkChannel([]string{"1"})
	unserved.PayType = "wxpay"
	for i, bad := range []PluginChannel{badApp, missing, unserved} {
		if _, e := BuildPluginChannel(bad); e == nil {
			t.Fatal("accepted bad", i)
		}
	}
}

func TestPluginAlipayhkMethodsAndCreate(t *testing.T) {
	// PC -> page, WAP -> wap, APP -> app.
	p, e := BuildPluginChannel(alipayhkChannel([]string{"1", "2", "3"}))
	if e != nil {
		t.Fatal(e)
	}
	got := p.Capabilities().Methods
	want := map[string]bool{"page": true, "wap": true, "app": true}
	if len(got) != 3 {
		t.Fatal("methods", got)
	}
	for _, m := range got {
		if !want[m] {
			t.Fatal("unexpected method", m)
		}
	}
	hk := p.(*alipayhkPlugin)

	// PC page: auto-submit form with MD5 sign to the gateway.
	r := CreateRequest{TradeNo: "T202610290001", Subject: "VIP", Method: "page", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 100}
	res, e := hk.Create(context.Background(), r)
	if e != nil || res.MaterialType != "html" {
		t.Fatal(res, e)
	}
	if !strings.Contains(res.Material, "intlmapi.alipay.com/gateway.do") || !strings.Contains(res.Material, `name="service"`) || !strings.Contains(res.Material, `name="sign"`) {
		t.Fatal("form", res.Material)
	}
	if !strings.Contains(res.Material, `value="create_forex_trade"`) || !strings.Contains(res.Material, `value="HKD"`) {
		t.Fatal("pc service/currency", res.Material)
	}

	// APP: signed SDK query string.
	r.Method = "app"
	res, e = hk.Create(context.Background(), r)
	if e != nil || res.MaterialType != "app" {
		t.Fatal(res, e)
	}
	q, _ := url.ParseQuery(res.Material)
	if q.Get("service") != "mobile.securitypay.pay" || q.Get("sign_type") != "MD5" || q.Get("sign") == "" {
		t.Fatal("app params", q)
	}

	// A method not enabled by app types is refused.
	p2, _ := BuildPluginChannel(alipayhkChannel([]string{"1"}))
	if _, e := p2.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "wap", NotifyURL: r.NotifyURL, Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("wap must be refused when not enabled", e)
	}
}

func TestPluginAlipayhkSignVector(t *testing.T) {
	p, _ := BuildPluginChannel(alipayhkChannel([]string{"1"}))
	hk := p.(*alipayhkPlugin)
	params := map[string]string{
		"partner": "2088000000000001", "notify_url": "https://pay.example/callbacks/providers/9/T202610290001",
		"return_url": "https://pay.example/checkout/T202610290001/return", "out_trade_no": "T202610290001",
		"subject": "VIP", "currency": "HKD", "rmb_fee": "1.00", "trade_status": "TRADE_SUCCESS",
		"trade_no": "2026HK0001", "buyer_id": "2088buyer", "sign": "x", "sign_type": "MD5",
	}
	// Independently computed with node (p03vec.js).
	if got := hk.alipayhkSign(params); got != "819206a3336495d0d4a3f1a6a1e83aa9" {
		t.Fatal(got)
	}
}

func TestPluginAlipayhkVerifyNotification(t *testing.T) {
	p, _ := BuildPluginChannel(alipayhkChannel([]string{"1"}))
	hk := p.(*alipayhkPlugin)
	base := map[string]string{
		"partner": "2088000000000001", "notify_url": "https://pay.example/callbacks/providers/9/T202610290001",
		"return_url": "https://pay.example/checkout/T202610290001/return", "out_trade_no": "T202610290001",
		"subject": "VIP", "currency": "HKD", "rmb_fee": "1.00", "trade_status": "TRADE_SUCCESS",
		"trade_no": "2026HK0001", "buyer_id": "2088buyer",
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
	good["sign"] = "819206a3336495d0d4a3f1a6a1e83aa9"
	good["sign_type"] = "MD5"
	exp := ExpectedPayment{TradeNo: "T202610290001", Amount: 100}
	ev, e := hk.VerifyNotification(context.Background(), nil, encode(good), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "2026HK0001" || ev.BuyerID != "2088buyer" || ev.Amount != 100 || ev.Currency != "CNY" {
		t.Fatal(ev, e)
	}
	tamper := func(k, v string) map[string]string {
		m := map[string]string{}
		for kk, vv := range good {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	for name, m := range map[string]map[string]string{
		"bad sign":   tamper("sign", strings.Repeat("0", 32)),
		"rmb edited": tamper("rmb_fee", "100.00"),
	} {
		if _, e := hk.VerifyNotification(context.Background(), nil, encode(m), exp); !errors.Is(e, ErrAuthentication) {
			t.Fatal(name, e)
		}
	}
	// Correctly signed but wrong amount / order.
	if _, e := hk.VerifyNotification(context.Background(), nil, encode(good), ExpectedPayment{TradeNo: "T202610290001", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e := hk.VerifyNotification(context.Background(), nil, encode(good), ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("order", e)
	}
	// Non-success trade_status.
	ns := tamper("trade_status", "WAIT_BUYER_PAY")
	ns["sign"] = hk.alipayhkSign(ns)
	if _, e := hk.VerifyNotification(context.Background(), nil, encode(ns), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success", e)
	}
	// Repeated keys refused.
	if _, e := hk.VerifyNotification(context.Background(), nil, []byte("a=1&a=2"), exp); e == nil {
		t.Fatal("repeated keys accepted")
	}
	if got := hk.NotificationMethods(); len(got) != 2 || got[0] != http.MethodGet {
		t.Fatal(got)
	}
}

func TestPluginAlipayhkRefund(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(b))
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?><alipay><is_success>T</is_success></alipay>`)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(alipayhkChannel([]string{"1"}))
	hk := p.(*alipayhkPlugin)
	hk.refundGatewayOverride = srv.URL
	rr, e := hk.Refund(context.Background(), RefundRequest{TradeNo: "T1", RefundNo: "R1", Amount: 500, TotalAmount: 1000})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 500 {
		t.Fatal(rr, e)
	}
	if form.Get("service") != "forex_refund" || form.Get("out_return_no") != "R1" || form.Get("sign") == "" {
		t.Fatal("refund request", form)
	}
	// Failure reply.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<alipay><is_success>F</is_success><error>ILLEGAL_SIGN</error></alipay>`)
	}))
	defer srv2.Close()
	hk.refundGatewayOverride = srv2.URL
	var pe *ProviderError
	if _, e := hk.Refund(context.Background(), RefundRequest{TradeNo: "T1", RefundNo: "R1", Amount: 500, TotalAmount: 1000}); !errors.As(e, &pe) {
		t.Fatal("failure must be definitive", e)
	}
}
