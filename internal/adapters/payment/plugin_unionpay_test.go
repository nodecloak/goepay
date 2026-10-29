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

func unionpayChannel(gw, payType string, appTypes []string) PluginChannel {
	return PluginChannel{ID: 11, Plugin: "unionpay", PayType: payType, AppTypes: appTypes, Config: map[string]string{"appid": "10001", "appkey": "ukey", "appurl": gw}}
}

func TestPluginUnionpayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("unionpay")
	if !ok || spec.Title != "银联前置" || !spec.Serves("wxpay") || !spec.Serves("jdpay") || spec.Serves("douyinpay") {
		t.Fatal("unionpay spec", ok)
	}
	if _, e := BuildPluginChannel(unionpayChannel("", "wxpay", nil)); e != nil {
		t.Fatal("default gateway must build", e)
	}
	bad := []PluginChannel{
		unionpayChannel("ftp://x/", "wxpay", nil),             // scheme
		unionpayChannel("https://g/", "douyinpay", nil),       // type not served
		unionpayChannel("https://g/", "wxpay", []string{"x"}), // unknown apptype
	}
	missing := unionpayChannel("https://g/", "wxpay", nil)
	delete(missing.Config, "appkey")
	bad = append(bad, missing)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginUnionpaySignatureVector(t *testing.T) {
	p, _ := BuildPluginChannel(unionpayChannel("https://g/", "wxpay", nil))
	params := map[string]string{"mch_id": "10001", "version": "2.0", "sign_type": "MD5", "nonce_str": "abc", "service": "unified.trade.native", "out_trade_no": "T1", "body": "VIP", "total_fee": "100", "mch_create_ip": "1.1.1.1", "notify_url": "https://pay.example/n", "sign": "x"}
	// MD5upper(ksort k=v& + "&key=ukey") computed independently with node.
	if got := p.(*unionpayPlugin).sign(params); got != "69747213895FCEBB295EEA4E3421A8E9" {
		t.Fatal(got)
	}
}

func TestPluginUnionpayCreateNative(t *testing.T) {
	var body string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "<service><![CDATA[unified.trade.native]]></service>") || !strings.Contains(string(b), "<out_trade_no><![CDATA[T1]]></out_trade_no>") {
			t.Errorf("request body %s", b)
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	defer srv.Close()
	p, e := BuildPluginChannel(unionpayChannel(srv.URL, "wxpay", []string{"scan"}))
	if e != nil {
		t.Fatal(e)
	}
	up := p.(*unionpayPlugin)
	r := CreateRequest{TradeNo: "T1", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100, ClientIP: "1.1.1.1"}

	reply := map[string]string{"status": "0", "result_code": "0", "code_url": "weixin://wxpay/bizpayurl?pr=abc", "transaction_id": "U1", "mch_id": "10001"}
	reply["sign"] = up.sign(reply)
	status, body = 200, unionpayXML(reply)
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "weixin://wxpay/bizpayurl?pr=abc" || res.ProviderTradeNo != "U1" {
		t.Fatal(res, e)
	}

	// business failure -> definitive
	fail := map[string]string{"status": "0", "result_code": "1", "err_code": "SYSTEMERROR"}
	fail["sign"] = up.sign(fail)
	status, body = 200, unionpayXML(fail)
	var pe *ProviderError
	if _, e = p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business failure must be definitive", e)
	}
	// bad reply signature -> unknown outcome
	status, body = 200, unionpayXML(map[string]string{"status": "0", "result_code": "0", "code_url": "x", "sign": "DEADBEEF"})
	var unknown *UnknownOutcome
	if _, e = p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("bad sign must be unknown", e)
	}
	// 5xx -> unknown
	status, body = 502, "bad"
	if _, e = p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("5xx must be unknown", e)
	}
}

func TestPluginUnionpayMethodsPerType(t *testing.T) {
	p, _ := BuildPluginChannel(unionpayChannel("https://g/", "wxpay", []string{"scan", "jsapi", "h5"}))
	m := p.Capabilities().Methods
	if !strings.Contains(strings.Join(m, ","), "native") || !strings.Contains(strings.Join(m, ","), "h5") || !strings.Contains(strings.Join(m, ","), "jsapi") {
		t.Fatal("wxpay methods", m)
	}
	// jdpay offers only native even with jsapi/h5 selected.
	p2, _ := BuildPluginChannel(unionpayChannel("https://g/", "jdpay", []string{"scan", "jsapi", "h5"}))
	if got := p2.Capabilities().Methods; len(got) != 1 || got[0] != "native" {
		t.Fatal("jdpay methods", got)
	}
	// jsapi needs an openid.
	if e := p.(*unionpayPlugin).ValidateCreate(CreateRequest{TradeNo: "T", Subject: "s", Amount: 1, Method: "jsapi", NotifyURL: "https://pay.example/n"}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi without openid", e)
	}
}

func TestPluginUnionpayNotificationChecks(t *testing.T) {
	p, _ := BuildPluginChannel(unionpayChannel("https://g/", "wxpay", nil))
	up := p.(*unionpayPlugin)
	base := map[string]string{"status": "0", "result_code": "0", "out_trade_no": "T1", "total_fee": "100", "transaction_id": "U1", "openid": "o-1", "mch_id": "10001"}
	base["sign"] = up.sign(base)
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := up.VerifyNotification(context.Background(), nil, []byte(unionpayXML(base)), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "U1" || ev.Amount != 100 || ev.BuyerID != "o-1" {
		t.Fatal(ev, e)
	}
	// wrong amount
	if _, e = up.VerifyNotification(context.Background(), nil, []byte(unionpayXML(base)), ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	// wrong trade
	if _, e = up.VerifyNotification(context.Background(), nil, []byte(unionpayXML(base)), ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// bad sign
	tampered := map[string]string{}
	for k, v := range base {
		tampered[k] = v
	}
	tampered["sign"] = strings.Repeat("A", 32)
	if _, e = up.VerifyNotification(context.Background(), nil, []byte(unionpayXML(tampered)), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// non-success status
	ns := map[string]string{"status": "0", "result_code": "1", "out_trade_no": "T1", "total_fee": "100"}
	ns["sign"] = up.sign(ns)
	if _, e = up.VerifyNotification(context.Background(), nil, []byte(unionpayXML(ns)), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("status", e)
	}
	if st, _, bdy := up.NotificationAck(nil); st != 200 || bdy != "success" {
		t.Fatal(st, bdy)
	}
	if _, _, bdy := up.NotificationAck(ErrInvalid); bdy != "failure" {
		t.Fatal(bdy)
	}
}

func TestPluginUnionpayRefund(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "unified.trade.refund") {
			t.Errorf("refund body %s", b)
		}
		io.WriteString(w, body)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(unionpayChannel(srv.URL, "wxpay", nil))
	up := p.(*unionpayPlugin)
	reply := map[string]string{"status": "0", "result_code": "0", "refund_id": "R-UP"}
	reply["sign"] = up.sign(reply)
	body = unionpayXML(reply)
	rr, e := p.Refund(context.Background(), RefundRequest{ProviderTradeNo: "U1", RefundNo: "R1", Amount: 50, TotalAmount: 100})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 50 || rr.ProviderRefundNo != "R-UP" {
		t.Fatal(rr, e)
	}
}
