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

func qqpayChannel(payType string) PluginChannel {
	return PluginChannel{ID: 12, Plugin: "qqpay", PayType: payType, Config: map[string]string{"appid": "20001", "appkey": "qkey", "wxappid": "wx123"}}
}

func TestPluginQqpayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("qqpay")
	if !ok || spec.Title != "QQ钱包官方支付" || !spec.Serves("qqpay") || spec.Serves("alipay") {
		t.Fatal("qqpay spec", ok)
	}
	if _, e := BuildPluginChannel(qqpayChannel("qqpay")); e != nil {
		t.Fatal(e)
	}
	// pay type not served
	if _, e := BuildPluginChannel(PluginChannel{Plugin: "qqpay", PayType: "alipay", Config: map[string]string{"appid": "1", "appkey": "k"}}); e == nil {
		t.Fatal("served alipay")
	}
	// missing required appkey
	missing := qqpayChannel("qqpay")
	delete(missing.Config, "appkey")
	if _, e := BuildPluginChannel(missing); e == nil {
		t.Fatal("accepted missing appkey")
	}
	// refund unsupported without cert
	p, _ := BuildPluginChannel(qqpayChannel("qqpay"))
	if p.Capabilities().Refund {
		t.Fatal("refund should be off without cert")
	}
	if _, e := p.Refund(context.Background(), RefundRequest{ProviderTradeNo: "x", RefundNo: "r", Amount: 1}); !errors.Is(e, ErrUnsupported) {
		t.Fatal("refund without cert", e)
	}
}

func TestPluginQqpaySignatureVector(t *testing.T) {
	p, _ := BuildPluginChannel(qqpayChannel("qqpay"))
	params := map[string]string{"mch_id": "20001", "nonce_str": "n1", "out_trade_no": "Q1", "body": "商品", "fee_type": "CNY", "notify_url": "https://pay.example/n", "spbill_create_ip": "2.2.2.2", "total_fee": "1000", "trade_type": "NATIVE", "sign": "x"}
	// MD5upper(ksort k=v& + "&key=qkey") computed independently with node.
	if got := p.(*qqpayPlugin).sign(params); got != "022A18CD03F1B14587B998F65183F821" {
		t.Fatal(got)
	}
}

func TestPluginQqpayCreateNativeAndJsapi(t *testing.T) {
	var body string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, body)
	}))
	defer srv.Close()
	// Point unified order at the test server via a channel-local override is
	// not possible (URLs are constants); instead drive the handler directly.
	p, _ := BuildPluginChannel(qqpayChannel("qqpay"))
	qp := p.(*qqpayPlugin)

	// Exercise the reply-mapping and signing logic through call() against the
	// test server by temporarily using its URL.
	reply := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "code_url": "https://myun.tenpay.com/q", "prepay_id": "PP1", "mch_id": "20001"}
	reply["sign"] = qp.sign(reply)
	body = qqpayXML(reply)
	out, e := qp.call(context.Background(), "t", srv.URL, map[string]string{"mch_id": "20001", "nonce_str": "n", "trade_type": "NATIVE", "out_trade_no": "Q1", "total_fee": "100", "body": "x"}, false)
	if e != nil || out["code_url"] != "https://myun.tenpay.com/q" || out["prepay_id"] != "PP1" {
		t.Fatal(out, e)
	}
	if !strings.Contains(gotBody, "<out_trade_no><![CDATA[Q1]]></out_trade_no>") || !strings.Contains(gotBody, "<sign>") {
		t.Fatal("request", gotBody)
	}
	// business failure (result_code FAIL)
	fail := map[string]string{"return_code": "SUCCESS", "result_code": "FAIL", "err_code": "PARAM_ERROR"}
	fail["sign"] = qp.sign(fail)
	body = qqpayXML(fail)
	var pe *ProviderError
	if _, e = qp.call(context.Background(), "t", srv.URL, map[string]string{"mch_id": "20001", "nonce_str": "n"}, false); !errors.As(e, &pe) {
		t.Fatal("business failure", e)
	}
}

func TestPluginQqpayNotificationChecks(t *testing.T) {
	p, _ := BuildPluginChannel(qqpayChannel("qqpay"))
	qp := p.(*qqpayPlugin)
	base := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "out_trade_no": "Q1", "total_fee": "1000", "transaction_id": "T-QQ", "openid": "o-1", "mch_id": "20001"}
	base["sign"] = qp.sign(base)
	exp := ExpectedPayment{TradeNo: "Q1", Amount: 1000}
	ev, e := qp.VerifyNotification(context.Background(), nil, []byte(qqpayXML(base)), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "T-QQ" || ev.Amount != 1000 || ev.BuyerID != "o-1" {
		t.Fatal(ev, e)
	}
	if _, e = qp.VerifyNotification(context.Background(), nil, []byte(qqpayXML(base)), ExpectedPayment{TradeNo: "Q1", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e = qp.VerifyNotification(context.Background(), nil, []byte(qqpayXML(base)), ExpectedPayment{TradeNo: "OTHER", Amount: 1000}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	tampered := map[string]string{}
	for k, v := range base {
		tampered[k] = v
	}
	tampered["sign"] = strings.Repeat("0", 32)
	if _, e = qp.VerifyNotification(context.Background(), nil, []byte(qqpayXML(tampered)), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	ns := map[string]string{"return_code": "SUCCESS", "result_code": "FAIL", "out_trade_no": "Q1", "total_fee": "1000"}
	ns["sign"] = qp.sign(ns)
	if _, e = qp.VerifyNotification(context.Background(), nil, []byte(qqpayXML(ns)), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("status", e)
	}
}

func TestPluginQqpayAckAndAppType(t *testing.T) {
	p, _ := BuildPluginChannel(qqpayChannel("qqpay"))
	if st, _, bdy := p.(NotificationAcker).NotificationAck(nil); st != 200 || bdy != "success" {
		t.Fatal(st, bdy)
	}
	// jsapi needs wxappid: a channel without it rejects jsapi create.
	noapp := qqpayChannel("qqpay")
	delete(noapp.Config, "wxappid")
	p2, _ := BuildPluginChannel(noapp)
	if e := p2.(*qqpayPlugin).ValidateCreate(CreateRequest{TradeNo: "Q", Subject: "s", Amount: 1, Method: "jsapi", NotifyURL: "https://pay.example/n"}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi without wxappid", e)
	}
}
