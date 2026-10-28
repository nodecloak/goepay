package payment

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func duolabaoChannel(payType string, apps ...string) PluginChannel {
	return PluginChannel{ID: 13, Plugin: "duolabao", PayType: payType, AppTypes: apps,
		Config: map[string]string{"customerNum": "C1", "accessKey": "ak", "secretKey": "dlb-secret", "shopNum": "", "agentNum": ""}}
}

func TestPluginDuolabaoRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("duolabao")
	if !ok || spec.Title != "哆啦宝支付" || !spec.Serves("jdpay") || spec.Serves("paypal") {
		t.Fatal("duolabao spec", ok)
	}
	if _, e := BuildPluginChannel(duolabaoChannel("alipay")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{duolabaoChannel("paypal")}
	for _, miss := range []string{"customerNum", "accessKey", "secretKey"} {
		c := duolabaoChannel("alipay")
		delete(c.Config, miss)
		bad = append(bad, c)
	}
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginDuolabaoTokenVector(t *testing.T) {
	p, _ := BuildPluginChannel(duolabaoChannel("alipay"))
	dp := p.(*duolabaoPlugin)
	body := `{"version":"V4.0","requestNum":"T1"}`
	// Independently computed with node crypto: upper(sha1("secretKey=..&timestamp=..&path=..&body=..")).
	if got := dp.token("1700000000", "/api/generateQRCodeUrl", body); got != "EE0C89D264734258C3B0062FA2E096F0FADC5F03" {
		t.Fatal(got)
	}
	// callback token drops path.
	cb := `{"status":"SUCCESS","requestNum":"T1","orderAmount":"1.23","orderNum":"D9","subOpenId":"o-x"}`
	if got := dp.token("1700000001", "", cb); got != "F12C71C09C0F8C45AA8758E440748443947854FE" {
		t.Fatal(got)
	}
}

func TestPluginDuolabaoCreateQR(t *testing.T) {
	var reply string
	var status int
	var hdr http.Header
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := mustDuolabao(t, srv.URL, "alipay")
	dp := p.(*duolabaoPlugin)
	status, reply = 200, `{"success":true,"url":"https://qr.dlb/U1"}`
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 123, ClientIP: "1.2.3.4"}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.dlb/U1" {
		t.Fatal(res, e)
	}
	// token recomputed from captured header timestamp + body (sorted JSON).
	want := dp.token(hdr.Get("timestamp"), "/api/generateQRCodeUrl", body)
	if hdr.Get("token") != want || hdr.Get("accessKey") != "ak" {
		t.Fatal("auth headers", hdr.Get("token"), want)
	}
}

func TestPluginDuolabaoCreateJSAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"success":true,"bankRequest":{"APPID":"wx1","PAYSIGN":"s"}}`)
	}))
	defer srv.Close()
	p := mustDuolabao(t, srv.URL, "wxpay", "2")
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 123, OpenID: "o-x"})
	if e != nil || res.MaterialType != "jsapi" || !strings.Contains(res.Material, `"APPID":"wx1"`) {
		t.Fatal(res, e)
	}
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi needs openid", e)
	}
}

func TestPluginDuolabaoCreateOutcomes(t *testing.T) {
	var reply string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := mustDuolabao(t, srv.URL, "alipay")
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 123}
	status, reply = 200, `{"errorCode":"E100","errorMsg":"店铺不存在"}`
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
	var unknown *UnknownOutcome
	for _, c := range []struct {
		s int
		b string
	}{{200, "not json"}, {503, "boom"}, {200, `{"foo":"bar"}`}} {
		status, reply = c.s, c.b
		if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
			t.Fatal("ambiguous must be unknown", c, e)
		}
	}
}

func TestPluginDuolabaoNotification(t *testing.T) {
	p, _ := BuildPluginChannel(duolabaoChannel("alipay"))
	dp := p.(*duolabaoPlugin)
	v := p.(NotificationVerifier)
	raw := []byte(`{"status":"SUCCESS","requestNum":"T1","orderAmount":"1.23","orderNum":"D9","subOpenId":"o-x","bankOutTradeNum":"B9"}`)
	hdr := func(ts, tok string) http.Header {
		h := http.Header{}
		h.Set("Timestamp", ts)
		h.Set("Token", tok)
		return h
	}
	ts := "1700000002"
	good := hdr(ts, dp.token(ts, "", string(raw)))
	exp := ExpectedPayment{TradeNo: "T1", Amount: 123}
	ev, e := v.VerifyNotification(context.Background(), good, raw, exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "D9" || ev.Amount != 123 || ev.BuyerID != "o-x" {
		t.Fatal(ev, e)
	}
	if _, e := v.VerifyNotification(context.Background(), hdr(ts, strings.Repeat("0", 40)), raw, exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad token accepted")
	}
	if _, e := v.VerifyNotification(context.Background(), good, raw, ExpectedPayment{TradeNo: "T1", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong amount accepted")
	}
	if _, e := v.VerifyNotification(context.Background(), good, raw, ExpectedPayment{TradeNo: "OTHER", Amount: 123}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong trade accepted")
	}
	closed := []byte(`{"status":"CLOSED","requestNum":"T1","orderAmount":"1.23"}`)
	ch := hdr(ts, dp.token(ts, "", string(closed)))
	if _, e := v.VerifyNotification(context.Background(), ch, closed, exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success accepted", e)
	}
	if s, _, b := p.(*duolabaoPlugin).NotificationAck(nil); s != 200 || b != "success" {
		t.Fatal(s, b)
	}
}

func TestPluginDuolabaoRefund(t *testing.T) {
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := mustDuolabao(t, srv.URL, "alipay")
	reply = `{"success":true,"orderNum":"D9","refundAmount":"1.00"}`
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", RefundNo: "R1", Amount: 100, TotalAmount: 123})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 100 {
		t.Fatal(rr, e)
	}
	reply = `{"errorCode":"E200","errorMsg":"退款失败"}`
	var pe *ProviderError
	if _, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", RefundNo: "R1", Amount: 100, TotalAmount: 123}); !errors.As(e, &pe) {
		t.Fatal(e)
	}
}

func mustDuolabao(t *testing.T, gateway, payType string, apps ...string) Provider {
	t.Helper()
	p, e := BuildPluginChannel(duolabaoChannel(payType, apps...))
	if e != nil {
		t.Fatal(e)
	}
	p.(*duolabaoPlugin).gateway = gateway
	return p
}
