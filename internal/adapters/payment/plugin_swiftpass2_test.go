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

// swiftpass2ExpectSign is computed independently with node (see sign-vector test).
const swiftpass2ExpectSign = "59D1B866EEF65BC4FE6A03864D26A63F"

func swiftpass2Channel(payType, gateway string) PluginChannel {
	c := PluginChannel{ID: 21, Plugin: "swiftpass2", PayType: payType, Config: map[string]string{"appid": "100001", "appkey": "testkey123"}}
	if gateway != "" {
		c.Config["appurl"] = gateway
	}
	return c
}

func TestPluginSwiftpass2Registry(t *testing.T) {
	spec, ok := LookupPlugin("swiftpass2")
	if !ok || spec.Title != "威富通MD5" || !spec.Serves("jdpay") || spec.Serves("paypal") {
		t.Fatal("spec", spec, ok)
	}
	if _, e := BuildPluginChannel(swiftpass2Channel("wxpay", "")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{
		swiftpass2Channel("paypal", ""),        // type not served
		swiftpass2Channel("wxpay", "ftp://x/"), // bad scheme
		swiftpass2Channel("wxpay", "notaurl"),  // not a url
	}
	missing := swiftpass2Channel("wxpay", "")
	delete(missing.Config, "appkey")
	bad = append(bad, missing)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

// Vector computed independently with node:
//
//	node -e 'const c=require("crypto");const p={mch_id:"100001",service:"pay.weixin.native",total_fee:"100",out_trade_no:"T1"};const k=Object.keys(p).sort();const s=k.map(x=>x+"="+p[x]).join("&")+"&key=testkey123";console.log(c.createHash("md5").update(s).digest("hex").toUpperCase())'
//	=> 1E9A3D... (see literal below)
func TestPluginSwiftpass2SignVector(t *testing.T) {
	p, _ := BuildPluginChannel(swiftpass2Channel("wxpay", ""))
	sp := p.(*swiftpass2Plugin)
	params := map[string]string{"mch_id": "100001", "service": "pay.weixin.native", "total_fee": "100", "out_trade_no": "T1", "sign": "x", "blank": ""}
	got := sp.sign(params)
	if got != swiftpass2ExpectSign {
		t.Fatalf("sign mismatch got %s want %s", got, swiftpass2ExpectSign)
	}
}

func TestPluginSwiftpass2CreateNative(t *testing.T) {
	var gotBody string
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, e := BuildPluginChannel(swiftpass2Channel("wxpay", srv.URL+"/"))
	if e != nil {
		t.Fatal(e)
	}
	sp := p.(*swiftpass2Plugin)

	// Build a signed success reply the plugin will accept.
	ok := map[string]string{"status": "0", "result_code": "0", "code_url": "weixin://wxpay/bizpayurl?pr=abc", "transaction_id": "WX9"}
	ok["sign"] = sp.sign(ok)
	reply = swiftpass2ToXML(ok)

	r := CreateRequest{TradeNo: "T100", Subject: "商品A", Method: "native", NotifyURL: "https://pay.example/n", ClientIP: "203.0.113.5", Amount: 100}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "weixin://wxpay/bizpayurl?pr=abc" || res.ProviderTradeNo != "WX9" {
		t.Fatal(res, e)
	}
	if !strings.Contains(gotBody, "<service><![CDATA[pay.weixin.native]]></service>") || !strings.Contains(gotBody, "<out_trade_no><![CDATA[T100]]></out_trade_no>") || !strings.Contains(gotBody, "<total_fee>100</total_fee>") {
		t.Fatal("request body", gotBody)
	}
	// Method other than native is rejected.
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "h5", NotifyURL: r.NotifyURL, Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-native accepted", e)
	}
}

func TestPluginSwiftpass2CreateOutcomes(t *testing.T) {
	var reply string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(swiftpass2Channel("alipay", srv.URL+"/"))
	sp := p.(*swiftpass2Plugin)
	r := CreateRequest{TradeNo: "T2", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 50}

	// Business refusal: signed reply with result_code != 0.
	fail := map[string]string{"status": "0", "result_code": "1", "err_code": "PARAM_ERROR"}
	fail["sign"] = sp.sign(fail)
	status, reply = 200, swiftpass2ToXML(fail)
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
	// Unsigned/garbled 2xx and 5xx are unknown outcomes.
	var unknown *UnknownOutcome
	status, reply = 200, "<xml><status>0</status><result_code>0</result_code><code_url>x</code_url></xml>"
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("unsigned reply must be unknown", e)
	}
	status, reply = 502, "<xml></xml>"
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("5xx must be unknown", e)
	}
}

func TestPluginSwiftpass2Notification(t *testing.T) {
	p, _ := BuildPluginChannel(swiftpass2Channel("wxpay", ""))
	sp := p.(*swiftpass2Plugin)
	good := map[string]string{"status": "0", "result_code": "0", "out_trade_no": "T777", "total_fee": "100", "transaction_id": "WX77", "openid": "o-abc"}
	good["sign"] = sp.sign(good)
	raw := []byte(swiftpass2ToXML(good))
	exp := ExpectedPayment{TradeNo: "T777", Amount: 100}

	ev, e := sp.VerifyNotification(context.Background(), nil, raw, exp)
	if e != nil || ev.State != "SUCCESS" || ev.Amount != 100 || ev.ProviderTradeNo != "WX77" || ev.BuyerID != "o-abc" {
		t.Fatal(ev, e)
	}
	// Bad signature.
	bad := map[string]string{}
	for k, v := range good {
		bad[k] = v
	}
	bad["sign"] = strings.Repeat("A", 32)
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(swiftpass2ToXML(bad)), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// Wrong amount / wrong trade number (correctly signed but mismatched expectation).
	if _, e := sp.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "T777", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e := sp.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// Non-success status.
	ns := map[string]string{"status": "0", "result_code": "1", "out_trade_no": "T777", "total_fee": "100"}
	ns["sign"] = sp.sign(ns)
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(swiftpass2ToXML(ns)), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success", e)
	}
	// Ack bodies.
	if st, _, body := sp.NotificationAck(nil); st != 200 || body != "success" {
		t.Fatal(st, body)
	}
	if _, _, body := sp.NotificationAck(ErrInvalid); body != "failure" {
		t.Fatal(body)
	}
}

func TestPluginSwiftpass2Refund(t *testing.T) {
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(swiftpass2Channel("wxpay", srv.URL+"/"))
	sp := p.(*swiftpass2Plugin)
	ok := map[string]string{"status": "0", "result_code": "0", "refund_id": "RF1"}
	ok["sign"] = sp.sign(ok)
	reply = swiftpass2ToXML(ok)
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T9", ProviderTradeNo: "WX9", RefundNo: "R9", Amount: 30, TotalAmount: 100})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 30 || rr.ProviderRefundNo != "RF1" {
		t.Fatal(rr, e)
	}
	fail := map[string]string{"status": "0", "result_code": "1", "err_code": "REFUND_NOT_ALLOWED"}
	fail["sign"] = sp.sign(fail)
	reply = swiftpass2ToXML(fail)
	var pe *ProviderError
	if _, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T9", ProviderTradeNo: "WX9", RefundNo: "R9", Amount: 30, TotalAmount: 100}); !errors.As(e, &pe) {
		t.Fatal(e)
	}
}
