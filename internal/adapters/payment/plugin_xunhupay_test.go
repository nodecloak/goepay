package payment

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func xunhupayChannel(api, payType string) PluginChannel {
	cfg := map[string]string{"appid": "1000", "appkey": "synthetic-key"}
	if api != "" {
		cfg["appurl"] = api
	}
	return PluginChannel{ID: 12, Plugin: "xunhupay", PayType: payType, Config: cfg}
}

func TestPluginXunhupayRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("xunhupay")
	if !ok || spec.Title != "虎皮椒支付" || !spec.Serves("alipay") || spec.Serves("qqpay") {
		t.Fatal("spec", ok)
	}
	if _, e := BuildPluginChannel(xunhupayChannel("", "wxpay")); e != nil {
		t.Fatal("default url", e)
	}
	bad := []PluginChannel{
		xunhupayChannel("https://api.example/payment/do.html", "qqpay"), // type not served
		xunhupayChannel("https://api.example/other", "alipay"),          // wrong path
		xunhupayChannel("ftp://api.example/payment/do.html", "alipay"),  // scheme
	}
	missing := xunhupayChannel("", "alipay")
	delete(missing.Config, "appkey")
	bad = append(bad, missing)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad", i)
		}
	}
}

func TestPluginXunhupayCreateAndSign(t *testing.T) {
	defer func(a func() int64, b func() string) { xunhupayNow, xunhupayNonce = a, b }(xunhupayNow, xunhupayNonce)
	xunhupayNow = func() int64 { return 1700000000 }
	xunhupayNonce = func() string { return "n1" }
	var body []byte
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(xunhupayChannel(srv.URL+"/payment/do.html", "alipay"))
	// QR flow: url_qrcode carries the real pay URL in data= (base64).
	payURL := "https://pay.example/go?x=1"
	qrURL := "https://qr.example/img?data=" + base64.StdEncoding.EncodeToString([]byte(payURL)) + "&t=1"
	reply = `{"errcode":0,"url":"https://m.example/h5","url_qrcode":"` + qrURL + `"}`
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 123})
	if e != nil || res.MaterialType != "qrcode" || res.Material != payURL {
		t.Fatal(res, e)
	}
	var sent map[string]any
	if e := json.Unmarshal(body, &sent); e != nil {
		t.Fatal(e)
	}
	// hash computed independently with node.
	if sent["hash"] != "dd5d9e61b2f8c39eca13ee1bcef2f286" {
		t.Fatalf("hash %v", sent["hash"])
	}
	if sent["payment"] != "alipay" || sent["total_fee"] != "1.23" || sent["trade_order_id"] != "T1" {
		t.Fatal("fields", sent)
	}
	// H5 flow returns a jump.
	res, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "h5", NotifyURL: "https://pay.example/n", Amount: 123})
	if e != nil || res.MaterialType != "jump" || res.Material != "https://m.example/h5" {
		t.Fatal(res, e)
	}
	// Business refusal and unknown outcomes.
	reply = `{"errcode":1,"errmsg":"x"}`
	var pe *ProviderError
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 123}); !errors.As(e, &pe) {
		t.Fatal("refusal", e)
	}
	reply = `not json`
	var unknown *UnknownOutcome
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 123}); !errors.As(e, &unknown) {
		t.Fatal("unknown", e)
	}
}

func TestPluginXunhupayNotification(t *testing.T) {
	p, _ := BuildPluginChannel(xunhupayChannel("", "alipay"))
	v := p.(NotificationVerifier)
	base := map[string]string{"trade_order_id": "T1", "status": "OD", "total_fee": "1.23", "open_order_id": "OO1", "appid": "1000"}
	base["hash"] = "92e858b4021790e27ad342634737b4f6" // computed independently
	encode := func(m map[string]string) []byte {
		q := url.Values{}
		for k, val := range m {
			q.Set(k, val)
		}
		return []byte(q.Encode())
	}
	exp := ExpectedPayment{TradeNo: "T1", Amount: 123}
	ev, e := v.VerifyNotification(context.Background(), nil, encode(base), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "OO1" || ev.Amount != 123 {
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
	if _, e = v.VerifyNotification(context.Background(), nil, encode(tamper("hash", strings.Repeat("0", 32))), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(tamper("total_fee", "9.99")), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(base), ExpectedPayment{TradeNo: "OTHER", Amount: 123}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade")
	}
	if status, _, body := p.(NotificationAcker).NotificationAck(nil); status != 200 || body != "success" {
		t.Fatal(status, body)
	}
}

func TestPluginXunhupayRefund(t *testing.T) {
	defer func(a func() int64, b func() string) { xunhupayNow, xunhupayNonce = a, b }(xunhupayNow, xunhupayNonce)
	xunhupayNow = func() int64 { return 1700000000 }
	xunhupayNonce = func() string { return "n1" }
	var path string
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(xunhupayChannel(srv.URL+"/payment/do.html", "wxpay"))
	reply = `{"errcode":0,"transaction_id":"X1","refund_fee":"1.00"}`
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "OO1", RefundNo: "R1", Amount: 100, TotalAmount: 123})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 100 || rr.ProviderRefundNo != "X1" || path != "/payment/refund.html" {
		t.Fatal(rr, e, path)
	}
	reply = `{"errcode":2,"errmsg":"x"}`
	var pe *ProviderError
	if _, e = p.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "OO1", RefundNo: "R1", Amount: 100, TotalAmount: 123}); !errors.As(e, &pe) {
		t.Fatal(e)
	}
}
