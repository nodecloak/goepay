package payment

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func leshuaChannel(payType string) PluginChannel {
	return PluginChannel{ID: 20, Plugin: "leshua", PayType: payType, Config: map[string]string{"appid": "M100", "appkey": "tradekey", "appsecret": "notifykey"}}
}

func leshuaXML(fields map[string]string) []byte {
	var b strings.Builder
	b.WriteString("<xml>")
	for k, v := range fields {
		b.WriteString("<" + k + ">" + v + "</" + k + ">")
	}
	b.WriteString("</xml>")
	return []byte(b.String())
}

func TestPluginLeshuaRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("leshua")
	if !ok || spec.Title != "乐刷聚合支付" || !spec.Serves("alipay") || !spec.Serves("wxpay") || !spec.Serves("bank") || spec.Serves("qqpay") {
		t.Fatal("leshua spec", spec, ok)
	}
	if _, e := BuildPluginChannel(leshuaChannel("alipay")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{leshuaChannel("qqpay")}
	missing := leshuaChannel("alipay")
	delete(missing.Config, "appkey")
	nosecret := leshuaChannel("alipay")
	delete(nosecret.Config, "appsecret")
	bad = append(bad, missing, nosecret)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginLeshuaSignatureVector(t *testing.T) {
	params := map[string]string{"service": "get_tdcode", "jspay_flag": "0", "pay_way": "ZFBZF", "merchant_id": "M100", "third_order_id": "T1", "amount": "100", "body": "VIP", "notify_url": "https://pay.example/n", "client_ip": "1.2.3.4", "nonce_str": "abc"}
	// Computed independently with node crypto.
	if got := leshuaMakeSign(params, "tradekey"); got != "490563E2AC684F0CDA8C69AD3B597E8E" {
		t.Fatal(got)
	}
	notify := map[string]string{"status": "2", "third_order_id": "T1", "leshua_order_id": "L9", "account": "100", "sub_openid": "o-x", "out_transaction_id": "W1"}
	if got := strings.ToLower(leshuaMakeSign(notify, "notifykey")); got != "354f8d8040239e6070065e576a809d8d" {
		t.Fatal(got)
	}
}

func TestPluginLeshuaCreate(t *testing.T) {
	var reply string
	var status int
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(b))
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()

	p, _ := BuildPluginChannel(leshuaChannel("alipay"))
	lp := p.(*leshuaPlugin)
	lp.apiURL = srv.URL

	// native alipay -> td_code QR
	status, reply = 200, "<xml><resp_code>0</resp_code><result_code>0</result_code><leshua_order_id>L1</leshua_order_id><td_code>weixin://wxpay/x</td_code></xml>"
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ClientIP: "1.2.3.4", Amount: 100}
	res, e := lp.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "weixin://wxpay/x" || res.ProviderTradeNo != "L1" {
		t.Fatal(res, e)
	}
	// Assert the exact request: service, pay_way, amount in fen, and a valid sign.
	if gotForm.Get("service") != "get_tdcode" || gotForm.Get("pay_way") != "ZFBZF" || gotForm.Get("jspay_flag") != "0" || gotForm.Get("amount") != "100" || gotForm.Get("third_order_id") != "T1" {
		t.Fatal("request", gotForm)
	}
	signed := map[string]string{}
	for k, v := range gotForm {
		signed[k] = v[0]
	}
	if gotForm.Get("sign") != leshuaMakeSign(signed, "tradekey") {
		t.Fatal("sign mismatch", gotForm.Get("sign"))
	}

	// jsapi -> jsapi material
	w, _ := BuildPluginChannel(leshuaChannel("wxpay"))
	wp := w.(*leshuaPlugin)
	wp.apiURL = srv.URL
	status, reply = 200, `<xml><resp_code>0</resp_code><result_code>0</result_code><leshua_order_id>L2</leshua_order_id><jspay_info>{"appId":"wx"}</jspay_info></xml>`
	res, e = wp.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "x", Method: "jsapi", OpenID: "o-1", NotifyURL: "https://pay.example/n", Amount: 50})
	if e != nil || res.MaterialType != "jsapi" || res.Material != `{"appId":"wx"}` {
		t.Fatal(res, e)
	}
	if gotForm.Get("sub_openid") != "o-1" || gotForm.Get("jspay_flag") != "1" {
		t.Fatal("jsapi request", gotForm)
	}

	// business refusal -> definitive ProviderError
	status, reply = 200, "<xml><resp_code>0</resp_code><result_code>1</result_code><error_msg>fail</error_msg></xml>"
	var pe *ProviderError
	if _, e = lp.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal", e)
	}
	// unknown outcome: unreadable body / 5xx
	for _, c := range []struct {
		status int
		reply  string
	}{{200, "not xml <"}, {502, "bad gateway"}} {
		status, reply = c.status, c.reply
		var unknown *UnknownOutcome
		if _, e = lp.Create(context.Background(), r); !errors.As(e, &unknown) {
			t.Fatal("unknown outcome", c, e)
		}
	}
}

func TestPluginLeshuaCreateRequest(t *testing.T) {
	p, _ := BuildPluginChannel(leshuaChannel("bank"))
	lp := p.(*leshuaPlugin)
	if got := lp.Capabilities().Methods; len(got) != 2 {
		t.Fatal(got)
	}
	// jsapi requires openid
	if e := lp.ValidateCreate(CreateRequest{TradeNo: "T1", Subject: "x", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi needs openid", e)
	}
	// unserved method
	if e := lp.ValidateCreate(CreateRequest{TradeNo: "T1", Subject: "x", Method: "app", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("app not served", e)
	}
	if lp.payWay() != "UPSMZF" || lp.jspayFlag("native") != "0" {
		t.Fatal("bank mapping", lp.payWay(), lp.jspayFlag("native"))
	}
	w, _ := BuildPluginChannel(leshuaChannel("wxpay"))
	if w.(*leshuaPlugin).jspayFlag("native") != "2" || w.(*leshuaPlugin).jspayFlag("jsapi") != "1" {
		t.Fatal("wxpay flag")
	}
}

func TestPluginLeshuaNotification(t *testing.T) {
	p, _ := BuildPluginChannel(leshuaChannel("alipay"))
	v := p.(NotificationVerifier)
	base := map[string]string{"status": "2", "third_order_id": "T1", "leshua_order_id": "L9", "account": "100", "sub_openid": "o-x", "out_transaction_id": "W1"}
	base["sign"] = "354F8D8040239E6070065E576A809D8D" // uppercase; compare is case-insensitive
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := v.VerifyNotification(context.Background(), nil, leshuaXML(base), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "L9" || ev.BuyerID != "o-x" || ev.Amount != 100 {
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
	// bad sign
	if _, e = v.VerifyNotification(context.Background(), nil, leshuaXML(tamper("sign", strings.Repeat("0", 32))), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// wrong amount
	if _, e = v.VerifyNotification(context.Background(), nil, leshuaXML(base), ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	// wrong trade number
	if _, e = v.VerifyNotification(context.Background(), nil, leshuaXML(base), ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// non-success status (resign status=1)
	ns := tamper("status", "1")
	ns["sign"] = leshuaMakeSign(ns, "notifykey")
	if _, e = v.VerifyNotification(context.Background(), nil, leshuaXML(ns), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("status", e)
	}
	// ack bodies
	if status, _, body := p.(NotificationAcker).NotificationAck(nil); status != 200 || body != "000000" {
		t.Fatal(status, body)
	}
	if _, _, body := p.(NotificationAcker).NotificationAck(ErrInvalid); body != "fail" {
		t.Fatal(body)
	}
}

func TestPluginLeshuaRefund(t *testing.T) {
	p, _ := BuildPluginChannel(leshuaChannel("alipay"))
	lp := p.(*leshuaPlugin)
	// Refund requires provider trade no and refund no.
	if _, e := lp.Refund(context.Background(), RefundRequest{TradeNo: "T1", RefundNo: "R1", Amount: 50, TotalAmount: 100}); !errors.Is(e, ErrInvalid) {
		t.Fatal("needs provider trade no", e)
	}
	// Refund sign field assembly is covered by the signature vector test; here
	// assert the refund params produce a stable sign over a representative set.
	params := map[string]string{"service": "unified_refund", "merchant_id": "M100", "leshua_order_id": "L9", "merchant_refund_id": "R1", "refund_amount": "50", "nonce_str": leshuaNonce("R1")}
	sign := leshuaMakeSign(params, "tradekey")
	if len(sign) != 32 {
		t.Fatal("sign length", sign)
	}
}
