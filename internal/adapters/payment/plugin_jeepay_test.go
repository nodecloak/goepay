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

func jeepayChannel(api, payType string) PluginChannel {
	return PluginChannel{ID: 90, Plugin: "jeepay", PayType: payType, Config: map[string]string{
		"appurl": api, "appmchid": "M1", "appid": "A1", "appkey": "secret-key",
	}}
}

func TestPluginJeepayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("jeepay")
	if !ok || spec.Title != "Jeepay聚合支付" || !spec.Serves("bank") || spec.Serves("ecny") {
		t.Fatal("jeepay spec", ok, spec.Title)
	}
	if _, e := BuildPluginChannel(jeepayChannel("https://up.ex/", "alipay")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{
		jeepayChannel("https://up.ex/", "ecny"),      // type not served
		jeepayChannel("https://up.ex/api", "alipay"), // must end with /
		jeepayChannel("ftp://up.ex/", "alipay"),      // scheme
	}
	missing := jeepayChannel("https://up.ex/", "alipay")
	delete(missing.Config, "appkey")
	bad = append(bad, missing)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginJeepaySignatureMatchesOriginal(t *testing.T) {
	p, _ := BuildPluginChannel(jeepayChannel("https://up.ex/", "alipay"))
	params := map[string]string{
		"mchNo": "M1", "appId": "A1", "mchOrderNo": "T1", "wayCode": "ALI_QR",
		"amount": "100", "currency": "cny", "subject": "商品", "body": "商品",
		"notifyUrl": "https://pay.ex/n", "version": "1.0", "signType": "MD5", "sign": "x",
	}
	// Independent node vector.
	if got := p.(*jeepayPlugin).sign(params); got != "C0B1A0F90645C3F76424446BE1D215A5" {
		t.Fatal(got)
	}
}

func TestPluginJeepayCreateQR(t *testing.T) {
	var reply string
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		if r.URL.Path != "/api/pay/unifiedOrder" || r.Method != http.MethodPost {
			w.WriteHeader(404)
			return
		}
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(jeepayChannel(srv.URL+"/", "alipay"))

	reply = `{"code":0,"data":{"payDataType":"codeUrl","payData":"https://qr.ex/U1","payOrderId":"P1"}}`
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "商品", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100, ClientIP: "1.2.3.4"})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.ex/U1" || res.ProviderTradeNo != "P1" {
		t.Fatal(res, e)
	}
	if !strings.Contains(got, `"wayCode":"ALI_QR"`) || !strings.Contains(got, `"amount":"100"`) || !strings.Contains(got, `"sign":`) {
		t.Fatal("request", got)
	}

	// payurl -> jump
	reply = `{"code":0,"data":{"payDataType":"payUrl","payData":"https://up.ex/pay/U1","payOrderId":"P1"}}`
	if res, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "wap", NotifyURL: "https://pay.ex/n", Amount: 100}); e != nil || res.MaterialType != "jump" {
		t.Fatal(res, e)
	}

	// definitive business refusal
	reply = `{"code":1,"msg":"通道维护"}`
	var pe *ProviderError
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100}); !errors.As(e, &pe) {
		t.Fatal("business refusal", e)
	}
	// data-level errMsg is definitive
	reply = `{"code":0,"data":{"errCode":"E01","errMsg":"下单失败"}}`
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100}); !errors.As(e, &pe) {
		t.Fatal("errMsg refusal", e)
	}
	// unknown outcomes
	for _, bodyReply := range []string{`not json`} {
		reply = bodyReply
		var unknown *UnknownOutcome
		if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100}); !errors.As(e, &unknown) {
			t.Fatal("ambiguous reply", e)
		}
	}
	// jsapi requires openid
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "jsapi", NotifyURL: "https://pay.ex/n", Amount: 100}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi needs openid", e)
	}
}

func TestPluginJeepayNotificationAndRefund(t *testing.T) {
	p, _ := BuildPluginChannel(jeepayChannel("https://up.ex/", "alipay"))
	v := p.(NotificationVerifier)
	// form callback
	good := "state=2&mchOrderNo=T1&payOrderId=P1&amount=100&sign=A3ABBD77E7F12A5D7D1DAFA5D22C2977"
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := v.VerifyNotification(context.Background(), nil, []byte(good), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "P1" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	bad := map[string]ExpectedPayment{
		"state=2&mchOrderNo=T1&payOrderId=P1&amount=100&sign=" + strings.Repeat("0", 32): exp,
	}
	for raw := range bad {
		if _, e = v.VerifyNotification(context.Background(), nil, []byte(raw), exp); !errors.Is(e, ErrAuthentication) {
			t.Fatal("bad sign accepted")
		}
	}
	// wrong amount / wrong trade no
	if _, e = v.VerifyNotification(context.Background(), nil, []byte(good), ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e = v.VerifyNotification(context.Background(), nil, []byte(good), ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// non-success state
	ns := "state=1&mchOrderNo=T1&amount=100&sign=x"
	if _, e = v.VerifyNotification(context.Background(), nil, []byte(ns), exp); !errors.Is(e, ErrAuthentication) && !errors.Is(e, ErrInvalid) {
		t.Fatal("non success", e)
	}
	// JSON callback
	jsonRaw := `{"state":"2","mchOrderNo":"T1","payOrderId":"P1","amount":"100","sign":"A3ABBD77E7F12A5D7D1DAFA5D22C2977"}`
	h := http.Header{"Content-Type": {"application/json"}}
	if ev, e = v.VerifyNotification(context.Background(), h, []byte(jsonRaw), exp); e != nil || ev.State != "SUCCESS" {
		t.Fatal("json callback", ev, e)
	}
	if status, _, body := p.(NotificationAcker).NotificationAck(nil); status != 200 || body != "success" {
		t.Fatal(status, body)
	}

	// Refund
	var rreply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, rreply)
	}))
	defer srv.Close()
	rp, _ := BuildPluginChannel(jeepayChannel(srv.URL+"/", "alipay"))
	rreply = `{"code":0,"data":{"refundOrderId":"RF1"}}`
	rr, e := rp.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "P1", RefundNo: "R1", Amount: 50, TotalAmount: 100})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 50 || rr.ProviderRefundNo != "RF1" {
		t.Fatal(rr, e)
	}
	rreply = `{"code":1,"msg":"失败"}`
	var pe *ProviderError
	if _, e = rp.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "P1", RefundNo: "R1", Amount: 50, TotalAmount: 100}); !errors.As(e, &pe) {
		t.Fatal("refund refusal", e)
	}
}
