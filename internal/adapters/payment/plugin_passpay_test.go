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

// Same fixed keypair as the epayn test (independently generated).
func passpayChannel(api, payType string, appTypes []string) PluginChannel {
	return PluginChannel{ID: 15, Plugin: "passpay", PayType: payType, AppTypes: appTypes, Config: map[string]string{
		"appurl": api, "appid": "1000", "appkey": epaynTestPriv, "appsecret": epaynTestPub, "appmchid": "C1",
	}}
}

func TestPluginPasspayRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("passpay")
	if !ok || spec.Title != "精秀支付" || !spec.Serves("bank") || spec.Serves("jdpay") {
		t.Fatal("spec", ok)
	}
	if _, e := BuildPluginChannel(passpayChannel("https://up.example/", "alipay", []string{"1"})); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{
		passpayChannel("https://up.example/", "jdpay", []string{"1"}),     // type not served
		passpayChannel("https://up.example/submit", "alipay", []string{}), // no trailing /
		passpayChannel("ftp://up.example/", "alipay", []string{}),         // scheme
	}
	missing := passpayChannel("https://up.example/", "alipay", []string{})
	delete(missing.Config, "appkey")
	badpub := passpayChannel("https://up.example/", "alipay", []string{})
	badpub.Config["appsecret"] = "not-a-key"
	bad = append(bad, missing, badpub)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad", i)
		}
	}
}

func TestPluginPasspayCreateAndSign(t *testing.T) {
	defer func(f func() int64) { passpayNow = f }(passpayNow)
	passpayNow = func() int64 { return 1700000000 }
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
	p, _ := BuildPluginChannel(passpayChannel(srv.URL+"/", "alipay", []string{"1"}))
	if got := p.Capabilities().Methods; len(got) != 1 || got[0] != "native" {
		t.Fatal(got)
	}
	reply = `{"code":1,"data":{"payurl":"https://qr.example/U1","trade_no":"U1"}}`
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", ClientIP: "192.0.2.1", Amount: 123})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.example/U1" || res.ProviderTradeNo != "U1" {
		t.Fatal(res, e)
	}
	// biz_content and the RSA2 sign match the independently computed vector.
	const wantBiz = `{"client_ip":"192.0.2.1","notify_url":"https://pay.example/n","out_trade_no":"T1","pay_channel_id":"C1","return_url":"https://pay.example/r","subject":"VIP","total_amount":"1.23","trade_type":"alipayQr"}`
	const wantSign = "EFYpdIGpayCWjto0ZJx7s6sModI5m1rCC8l+mYJKF5AuEYkUudaHyw+lmDiWnhn0pnZCpQtWpf1BYq7YGlxPhgXnkR+6AztxxDKKOEYiodmh+HDXiYpOzu5PY+fmRpuEYbTkxs+e0osBVwVFkzYzNo2bmOwIYtpKkwCRFHgZcS7+YFvuseG4dmLysGOrTlg5C/OdUuUsNnbC6JUNDdmdnp1POKYOy082wUMVzG/Dv26WFZAHr6ruC5DgiuKNQQcMDfu/9WUDCOja19yJibaLdDT6ZM2uOJOkpc3Y2Vo2n4dBtYS3epxDQUA1ei+RJ5Nd5XHz+7Af7Vq0O6B/LDLlcw=="
	if path != "/pay.order/create" || form.Get("biz_content") != wantBiz {
		t.Fatalf("biz_content\n got %s", form.Get("biz_content"))
	}
	if form.Get("sign") != wantSign {
		t.Fatalf("sign mismatch\n got %s", form.Get("sign"))
	}
	if form.Get("sign_type") != "RSA2" || form.Get("mchid") != "1000" {
		t.Fatal("envelope", form)
	}
	// Business refusal and unknown outcomes.
	reply = `{"code":0,"msg":"x"}`
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

func TestPluginPasspayJSApiRequiresOpenID(t *testing.T) {
	p, _ := BuildPluginChannel(passpayChannel("https://up.example/", "wxpay", []string{"2"}))
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 123}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi without openid should be invalid", e)
	}
}

func TestPluginPasspayNotification(t *testing.T) {
	p, _ := BuildPluginChannel(passpayChannel("https://up.example/", "alipay", []string{"1"}))
	v := p.(NotificationVerifier)
	base := map[string]string{"order_status": "SUCCESS", "out_trade_no": "T1", "trade_no": "U1", "total_amount": "1.23"}
	base["sign"] = "ARwb9lscfV1Ij6P/7EbR9V5zyrB/nZOM5pMmGWsLLi8oWPUOIxZ9kLu5WDBdNvpTBXEFx6ntgCk8jqZtDXJ8MaETBFfnROPWQ9rM97+osfIgTQNJ4zrp6pfwHWavVCZmMc4P5T0Z8MGUeO9H8iyIny78T7w9pqlO5XJ06X8G8YBbiqAzi3zgnYEB8jyzief/zp2pwSDnTktEeowFXvcSS30aOiHfwrYJpwdqi8zy6KmgyVjXwtLBY+Mhy/6KXQUo9+7pesDf76x4/hvp68ktTG3rsGmNcAja6X5/yw8BGWdG3ED2Wh1fdKhFLy3+6RLmQvJUvQll4x75IzJxIMr7DA=="
	encode := func(m map[string]string) []byte {
		q := url.Values{}
		for k, val := range m {
			q.Set(k, val)
		}
		return []byte(q.Encode())
	}
	exp := ExpectedPayment{TradeNo: "T1", Amount: 123}
	ev, e := v.VerifyNotification(context.Background(), nil, encode(base), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "U1" || ev.Amount != 123 {
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
	if _, e = v.VerifyNotification(context.Background(), nil, encode(tamper("sign", "AA==")), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign")
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(tamper("total_amount", "9.99")), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount")
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(base), ExpectedPayment{TradeNo: "OTHER", Amount: 123}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade")
	}
	if status, _, body := p.(NotificationAcker).NotificationAck(nil); status != 200 || body != "success" {
		t.Fatal(status, body)
	}
}

func TestPluginPasspayRefund(t *testing.T) {
	defer func(f func() int64) { passpayNow = f }(passpayNow)
	passpayNow = func() int64 { return 1700000000 }
	var path string
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(passpayChannel(srv.URL+"/", "alipay", []string{"1"}))
	reply = `{"code":1,"data":{"trade_no":"U1","refund_amount":"1.00"}}`
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "U1", RefundNo: "R1", Amount: 100, TotalAmount: 123})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 100 || rr.ProviderRefundNo != "U1" || path != "/pay.order/refund" {
		t.Fatal(rr, e, path)
	}
	reply = `{"code":0,"msg":"x"}`
	var pe *ProviderError
	if _, e = p.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "U1", RefundNo: "R1", Amount: 100, TotalAmount: 123}); !errors.As(e, &pe) {
		t.Fatal(e)
	}
}
