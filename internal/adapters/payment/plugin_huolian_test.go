package payment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func huolianChannel(payType, refundPwd string) PluginChannel {
	return PluginChannel{ID: 91, Plugin: "huolian", PayType: payType, Config: map[string]string{
		"appid": "AC", "appkey": "saltSALT", "appmchid": "MCH", "appurl": "13800000000", "appsecret": refundPwd,
	}}
}

func TestPluginHuolianRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("huolian")
	if !ok || spec.Title != "火脸支付" || !spec.Serves("wxpay") || spec.Serves("ecny") {
		t.Fatal("huolian spec", ok)
	}
	if _, e := BuildPluginChannel(huolianChannel("alipay", "")); e != nil {
		t.Fatal(e)
	}
	missing := huolianChannel("alipay", "")
	delete(missing.Config, "appmchid")
	if _, e := BuildPluginChannel(missing); e == nil {
		t.Fatal("accepted missing merchant id")
	}
	if _, e := BuildPluginChannel(huolianChannel("ecny", "")); e == nil {
		t.Fatal("accepted unserved type")
	}
	// Refund capability gated on refund password.
	p, _ := BuildPluginChannel(huolianChannel("alipay", ""))
	if p.Capabilities().Refund {
		t.Fatal("refund must be off without password")
	}
	p2, _ := BuildPluginChannel(huolianChannel("alipay", "pw"))
	if !p2.Capabilities().Refund {
		t.Fatal("refund must be on with password")
	}
}

func TestPluginHuolianSignatureMatchesOriginal(t *testing.T) {
	p, _ := BuildPluginChannel(huolianChannel("alipay", ""))
	merged := map[string]string{
		"authCode": "AC", "requestTime": "20261029010101", "resource": "api.hl.order.pay.native", "versionNo": "1",
		"businessOrderNo": "T1", "payAmount": "1.00", "merchantNo": "MCH", "operatorAccount": "13800000000",
		"notifyUrl": "https://pay.ex/n", "subject": "商品", "payWay": "alipay", "clientIp": "1.2.3.4",
	}
	if got := p.(*huolianPlugin).sign(merged); got != "74cf74078567d3bdf991d6273db41d8c" {
		t.Fatal(got)
	}
}

func TestPluginHuolianCreateNative(t *testing.T) {
	var reply string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	built, _ := BuildPluginChannel(huolianChannel("alipay", ""))
	p := built.(*huolianPlugin)
	p.gateway = srv.URL + "/do"

	reply = `{"code":0,"status":200,"data":{"orderNo":"O1","qrCodeUrl":"https://qr.ex/O1"}}`
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "商品", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100, ClientIP: "1.2.3.4"})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.ex/O1" || res.ProviderTradeNo != "O1" {
		t.Fatal(res, e)
	}
	if body["authCode"] != "AC" || body["sign"] == nil || body["params"] == nil {
		t.Fatal("envelope", body)
	}
	// h5 -> jump
	reply = `{"code":0,"status":200,"data":{"orderNo":"O2","payUrl":"https://up.ex/h5/O2"}}`
	if res, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "h5", NotifyURL: "https://pay.ex/n", ReturnURL: "https://pay.ex/r", Amount: 100}); e != nil || res.MaterialType != "jump" || res.Material != "https://up.ex/h5/O2" {
		t.Fatal(res, e)
	}
	// business refusal
	reply = `{"code":1,"status":200,"message":"失败"}`
	var pe *ProviderError
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100}); !errors.As(e, &pe) {
		t.Fatal("refusal", e)
	}
	// unknown
	reply = `not json`
	var unk *UnknownOutcome
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100}); !errors.As(e, &unk) {
		t.Fatal("unknown", e)
	}
}

func TestPluginHuolianNotification(t *testing.T) {
	p, _ := BuildPluginChannel(huolianChannel("alipay", ""))
	v := p.(NotificationVerifier)
	respBody := `{"orderStatus":2,"businessOrderNo":"T1","orderNo":"O1","payAmount":"1.00","userId":"u-1"}`
	obj := map[string]any{"code": 0, "message": "ok", "respBody": respBody, "bizType": "pay", "sign": "1d89124e1def3d33cdf82f4dca61c2a7"}
	raw, _ := json.Marshal(obj)
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := v.VerifyNotification(context.Background(), nil, raw, exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "O1" || ev.BuyerID != "u-1" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// bad sign
	obj["sign"] = strings.Repeat("0", 32)
	raw, _ = json.Marshal(obj)
	if _, e = v.VerifyNotification(context.Background(), nil, raw, exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// wrong amount
	obj["sign"] = "1d89124e1def3d33cdf82f4dca61c2a7"
	raw, _ = json.Marshal(obj)
	if _, e = v.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e = v.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	if status, _, body := p.(NotificationAcker).NotificationAck(nil); status != 200 || body != "SUCCESS" {
		t.Fatal(status, body)
	}
	if _, _, body := p.(NotificationAcker).NotificationAck(ErrInvalid); body != "FAIL" {
		t.Fatal("fail ack", body)
	}
}
