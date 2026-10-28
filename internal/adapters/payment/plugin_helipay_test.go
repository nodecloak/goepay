package payment

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func helipayChannel(payType string) PluginChannel {
	return PluginChannel{ID: 13, Plugin: "helipay", PayType: payType, Config: map[string]string{
		"appid": "C1000", "appkey": "signkey-abc", "appsecret": "enckey", "reportid": "RPT1",
	}}
}

// helipayExpectSign reproduces make_sign INDEPENDENTLY of the plugin code: join
// "&"+value for each entry, then "&"+key, md5-hex.
func helipayExpectSign(values []string, key string) string {
	var sb strings.Builder
	for _, v := range values {
		sb.WriteByte('&')
		sb.WriteString(v)
	}
	sb.WriteByte('&')
	sb.WriteString(key)
	sum := md5.Sum([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

func TestPluginHelipayRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("helipay")
	if !ok || !spec.Serves("qqpay") || spec.Serves("jdpay") {
		t.Fatal("helipay spec", ok)
	}
	if _, e := BuildPluginChannel(helipayChannel("wxpay")); e != nil {
		t.Fatal(e)
	}
	miss := helipayChannel("wxpay")
	delete(miss.Config, "appkey")
	for i, c := range []PluginChannel{miss, helipayChannel("jdpay")} {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatalf("accepted bad channel %d", i)
		}
	}
	sub := helipayChannel("alipay")
	sub.Config["appmchid"] = "SUB1"
	p, _ := BuildPluginChannel(sub)
	if p.(*helipayPlugin).custNo != "SUB1" {
		t.Fatal("appmchid not applied")
	}
}

func TestPluginHelipaySignVector(t *testing.T) {
	vals := []string{"AppPay", "T1", "C1000", "SCAN", "1.00", "CNY", "1", "WXPAY", "https://pay.example/n", "", "1.2.3.4", "sub", "", ""}
	want := helipayExpectSign(vals, "signkey-abc")
	ordered := []helipayKV{
		{"P1_bizType", "AppPay"}, {"P2_orderId", "T1"}, {"P3_customerNumber", "C1000"},
		{"P4_payType", "SCAN"}, {"P5_orderAmount", "1.00"}, {"P6_currency", "CNY"},
		{"P7_authcode", "1"}, {"P8_appType", "WXPAY"}, {"P9_notifyUrl", "https://pay.example/n"},
		{"P10_successToUrl", ""}, {"P11_orderIp", "1.2.3.4"}, {"P12_goodsName", "sub"},
		{"P13_goodsDetail", ""}, {"P14_desc", ""},
	}
	if got := helipaySign(ordered, "signkey-abc"); got != want {
		t.Fatalf("helipaySign = %s want %s", got, want)
	}
}

func TestPluginHelipayCreateNativeAndJsapi(t *testing.T) {
	var gotForm url.Values
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(b))
		io.WriteString(w, reply)
	}))
	defer srv.Close()

	p, _ := BuildPluginChannel(helipayChannel("wxpay"))
	hp := p.(*helipayPlugin)
	hp.client = srv.Client()
	oldURL := helipayAPIURL
	helipayAPIURL = srv.URL
	defer func() { helipayAPIURL = oldURL }()

	reply = `{"rt2_retCode":"0000","rt6_serialNumber":"S1","rt8_qrcode":"weixin://x"}`
	res, err := hp.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "sub", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100, ClientIP: "1.2.3.4"})
	if err != nil || res.MaterialType != "qrcode" || res.ProviderTradeNo != "S1" {
		t.Fatal(res, err)
	}
	vals := []string{gotForm.Get("P1_bizType"), gotForm.Get("P2_orderId"), gotForm.Get("P3_customerNumber"), gotForm.Get("P4_payType"), gotForm.Get("P5_orderAmount"), gotForm.Get("P6_currency"), gotForm.Get("P7_authcode"), gotForm.Get("P8_appType"), gotForm.Get("P9_notifyUrl"), gotForm.Get("P10_successToUrl"), gotForm.Get("P11_orderIp"), gotForm.Get("P12_goodsName"), gotForm.Get("P13_goodsDetail"), gotForm.Get("P14_desc"), gotForm.Get("P15_subMerchantId")}
	if gotForm.Get("sign") != helipayExpectSign(vals, "signkey-abc") {
		t.Fatal("request sign mismatch")
	}
	if gotForm.Get("P5_orderAmount") != "1.00" || gotForm.Get("P8_appType") != "WXPAY" {
		t.Fatal("request fields", gotForm)
	}

	if _, e := hp.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "x", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi needs openid", e)
	}
	reply = `{"rt2_retCode":"0001","rt6_serialNumber":"S2","rt10_payInfo":"{\"a\":1}"}`
	res, err = hp.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "x", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 1, OpenID: "o-1"})
	if err != nil || res.MaterialType != "jsapi" || res.Material != `{"a":1}` {
		t.Fatal(res, err)
	}

	reply = `{"rt2_retCode":"9999","rt3_retMsg":"closed"}`
	var pe *ProviderError
	if _, e := hp.Create(context.Background(), CreateRequest{TradeNo: "T3", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.As(e, &pe) {
		t.Fatal("expected refusal", e)
	}
	reply = `nope`
	var uo *UnknownOutcome
	if _, e := hp.Create(context.Background(), CreateRequest{TradeNo: "T4", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.As(e, &uo) {
		t.Fatal("expected unknown", e)
	}
}

func TestPluginHelipayVerifyNotification(t *testing.T) {
	p, _ := BuildPluginChannel(helipayChannel("alipay"))
	hp := p.(*helipayPlugin)
	base := map[string]string{
		"rt1_customerNumber": "C1000", "rt2_orderId": "T9", "rt3_systemSerial": "S9",
		"rt4_status": "SUCCESS", "rt5_orderAmount": "9.90", "rt6_currency": "CNY",
		"rt7_timestamp": "20261028", "rt8_desc": "ok", "rt19_subOpenId": "2088",
	}
	vals := []string{base["rt1_customerNumber"], base["rt2_orderId"], base["rt3_systemSerial"], base["rt4_status"], base["rt5_orderAmount"], base["rt6_currency"], base["rt7_timestamp"], base["rt8_desc"]}
	sign := helipayExpectSign(vals, "signkey-abc")
	enc := func(m map[string]string, sign string) []byte {
		q := url.Values{}
		for k, v := range m {
			q.Set(k, v)
		}
		q.Set("sign", sign)
		return []byte(q.Encode())
	}
	exp := ExpectedPayment{TradeNo: "T9", Amount: 990}
	ev, err := hp.VerifyNotification(context.Background(), nil, enc(base, sign), exp)
	if err != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "S9" || ev.Amount != 990 || ev.BuyerID != "2088" {
		t.Fatal(ev, err)
	}
	if _, e := hp.VerifyNotification(context.Background(), nil, enc(base, strings.Repeat("0", 32)), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign accepted", e)
	}
	if _, e := hp.VerifyNotification(context.Background(), nil, enc(base, sign), ExpectedPayment{TradeNo: "T9", Amount: 1}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong amount accepted", e)
	}
	if _, e := hp.VerifyNotification(context.Background(), nil, enc(base, sign), ExpectedPayment{TradeNo: "X", Amount: 990}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong trade accepted", e)
	}
	nb := map[string]string{}
	for k, v := range base {
		nb[k] = v
	}
	nb["rt4_status"] = "FAIL"
	nvals := []string{nb["rt1_customerNumber"], nb["rt2_orderId"], nb["rt3_systemSerial"], nb["rt4_status"], nb["rt5_orderAmount"], nb["rt6_currency"], nb["rt7_timestamp"], nb["rt8_desc"]}
	if _, e := hp.VerifyNotification(context.Background(), nil, enc(nb, helipayExpectSign(nvals, "signkey-abc")), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success accepted", e)
	}
	if s, _, b := hp.NotificationAck(nil); s != 200 || b != "success" {
		t.Fatal("ack", s, b)
	}
}

func TestPluginHelipayRefund(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(b))
		io.WriteString(w, `{"rt2_retCode":"0000","rt7_serialNumber":"RS1"}`)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(helipayChannel("wxpay"))
	hp := p.(*helipayPlugin)
	hp.client = srv.Client()
	oldURL := helipayAPIURL
	helipayAPIURL = srv.URL
	defer func() { helipayAPIURL = oldURL }()
	rr, err := hp.Refund(context.Background(), RefundRequest{TradeNo: "T1", RefundNo: "R1", Amount: 500, TotalAmount: 990})
	if err != nil || rr.State != "SUCCESS" || rr.Amount != 500 || rr.ProviderRefundNo != "RS1" {
		t.Fatal(rr, err)
	}
	if gotForm.Get("P4_refundOrderId") != "R1" || gotForm.Get("P5_amount") != "5.00" {
		t.Fatal("refund form", gotForm)
	}
}
