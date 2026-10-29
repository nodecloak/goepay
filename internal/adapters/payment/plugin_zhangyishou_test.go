package payment

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func zhangyishouChannel(payType, mchid string) PluginChannel {
	return PluginChannel{ID: 16, Plugin: "zhangyishou", PayType: payType, Config: map[string]string{
		"appid": "1000", "appkey": "synthetic-key", "appurl": "MNO1", "appmchid": mchid,
	}}
}

func TestPluginZhangyishouRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("zhangyishou")
	if !ok || spec.Title != "掌易收聚合支付" || !spec.Serves("bank") || spec.Serves("jdpay") {
		t.Fatal("spec", ok)
	}
	if _, e := BuildPluginChannel(zhangyishouChannel("alipay", "QR1")); e != nil {
		t.Fatal(e)
	}
	if _, e := BuildPluginChannel(zhangyishouChannel("jdpay", "QR1")); e == nil {
		t.Fatal("accepted unserved type")
	}
	for _, miss := range []string{"appid", "appkey", "appurl", "appmchid"} {
		c := zhangyishouChannel("alipay", "QR1")
		delete(c.Config, miss)
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted missing", miss)
		}
	}
	// wxpay with a | pair offers native + jsapi(scheme).
	p, _ := BuildPluginChannel(zhangyishouChannel("wxpay", "QR1|MINI1"))
	m := p.Capabilities().Methods
	if len(m) != 2 || m[0] != "native" || m[1] != "jsapi" {
		t.Fatal(m)
	}
}

// zhangyishou posts to a fixed host; redirect it to a test server by overriding
// the package-level host for the duration of the test is not possible (const),
// so these tests exercise sign/outcome mapping through a client whose transport
// rewrites the request to the httptest server.
type zysRewrite struct {
	base string
	rt   http.RoundTripper
}

func (z zysRewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	r.URL.Scheme = "http"
	r.URL.Host = z.base
	return z.rt.RoundTrip(r)
}

func zysTestClient(serverHost string) *http.Client {
	return &http.Client{Transport: zysRewrite{base: serverHost, rt: http.DefaultTransport}}
}

func TestPluginZhangyishouCreateAndSign(t *testing.T) {
	defer func(f func() time.Time) { zhangyishouNow = f }(zhangyishouNow)
	zhangyishouNow = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var body []byte
	var path string
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		body, _ = io.ReadAll(r.Body)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	host := srv.Listener.Addr().String()
	c := zhangyishouChannel("alipay", "QR1")
	c.HTTPClient = zysTestClient(host)
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	reply = `{"Code":"1009","Info":"https://qr.example/U1"}`
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ClientIP: "192.0.2.1", Amount: 123})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.example/U1" {
		t.Fatal(res, e)
	}
	if path != "/api/Order/AddOrder" {
		t.Fatal("path", path)
	}
	sent, _ := callbackJSON(body)
	// MD5Sign computed independently (positional concat + key).
	if jsonText(sent, "MD5Sign") != "28cdb263b9a17bef11b34a247e3ffddd" {
		t.Fatalf("sign %s", jsonText(sent, "MD5Sign"))
	}
	if jsonText(sent, "OrderMoney") != "1.23" || jsonText(sent, "MerchantNo") != "MNO1" || jsonText(sent, "Mproductdesc") != "VIP" {
		t.Fatal("fields", sent)
	}
	reply = `{"Code":"1001","Message":"x"}`
	var pe *ProviderError
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ClientIP: "192.0.2.1", Amount: 123}); !errors.As(e, &pe) {
		t.Fatal("refusal", e)
	}
	reply = `not json`
	var unknown *UnknownOutcome
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ClientIP: "192.0.2.1", Amount: 123}); !errors.As(e, &unknown) {
		t.Fatal("unknown", e)
	}
}

func TestPluginZhangyishouNotification(t *testing.T) {
	p, _ := BuildPluginChannel(zhangyishouChannel("alipay", "QR1"))
	v := p.(NotificationVerifier)
	// Signature = md5(MerchantId + DownstreamOrderNo + key), computed independently.
	body := func(sig string) []byte {
		return []byte(`{"MerchantId":"1000","DownstreamOrderNo":"T1","OrderState":1,"OrderNo":"U1","OrderMoney":"1.23","Signature":"` + sig + `"}`)
	}
	exp := ExpectedPayment{TradeNo: "T1", Amount: 123}
	ev, e := v.VerifyNotification(context.Background(), nil, body("ee2320083dacfd19e5a2512728d36b60"), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "U1" || ev.Amount != 123 {
		t.Fatal(ev, e)
	}
	if _, e = v.VerifyNotification(context.Background(), nil, body("00000000000000000000000000000000"), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign")
	}
	if _, e = v.VerifyNotification(context.Background(), nil, body("ee2320083dacfd19e5a2512728d36b60"), ExpectedPayment{TradeNo: "T1", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount")
	}
	if _, e = v.VerifyNotification(context.Background(), nil, body("ee2320083dacfd19e5a2512728d36b60"), ExpectedPayment{TradeNo: "OTHER", Amount: 123}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade")
	}
	// Non-success order state.
	bad := []byte(`{"MerchantId":"1000","DownstreamOrderNo":"T1","OrderState":0,"OrderMoney":"1.23","Signature":"ee2320083dacfd19e5a2512728d36b60"}`)
	if _, e = v.VerifyNotification(context.Background(), nil, bad, exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("state", e)
	}
	if status, _, bodyS := p.(NotificationAcker).NotificationAck(nil); status != 200 || bodyS != "OK" {
		t.Fatal(status, bodyS)
	}
}

func TestPluginZhangyishouRefund(t *testing.T) {
	zhangyishouNowReset := zhangyishouNow
	defer func() { zhangyishouNow = zhangyishouNowReset }()
	var path string
	var body []byte
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		body, _ = io.ReadAll(r.Body)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	host := srv.Listener.Addr().String()
	c := zhangyishouChannel("alipay", "QR1")
	c.HTTPClient = zysTestClient(host)
	p, _ := BuildPluginChannel(c)
	reply = `{"Code":"1009"}`
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", RefundNo: "R1", Amount: 123, TotalAmount: 123})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 123 || path != "/api/OrderRefund/Refund" {
		t.Fatal(rr, e, path)
	}
	sent, _ := callbackJSON(body)
	// md5(MerchantId + MerchantOrder + RefundAmount + key), computed independently.
	if jsonText(sent, "MD5Sign") != "e3b2558525ee07a06f09394490a6f908" {
		t.Fatalf("refund sign %s", jsonText(sent, "MD5Sign"))
	}
	reply = `{"Code":"1001","Message":"x"}`
	var pe *ProviderError
	if _, e = p.Refund(context.Background(), RefundRequest{TradeNo: "T1", RefundNo: "R1", Amount: 123, TotalAmount: 123}); !errors.As(e, &pe) {
		t.Fatal(e)
	}
}
