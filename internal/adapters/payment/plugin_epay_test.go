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

func epayChannel(api, switchMode, payType string) PluginChannel {
	return PluginChannel{ID: 9, Plugin: "epay", PayType: payType, Config: map[string]string{"appurl": api, "appid": "1000", "appkey": "synthetic-key", "appswitch": switchMode}}
}

func TestEpayPluginRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("epay")
	if !ok || spec.Title != "彩虹易支付" || !spec.Serves("wxpay") || spec.Serves("paypal") {
		t.Fatal("epay spec", spec, ok)
	}
	if _, e := BuildPluginChannel(epayChannel("https://up.example/", "0", "alipay")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{
		epayChannel("https://up.example/", "0", "paypal"),       // type not served
		epayChannel("https://up.example/submit", "0", "alipay"), // must end with /
		epayChannel("ftp://up.example/", "0", "alipay"),         // scheme
		epayChannel("https://up.example/", "2", "alipay"),       // option not offered
	}
	missing := epayChannel("https://up.example/", "0", "alipay")
	delete(missing.Config, "appkey")
	unknown := epayChannel("https://up.example/", "0", "alipay")
	unknown.Config["sub_mchid"] = "x"
	bad = append(bad, missing, unknown)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestEpaySignatureMatchesOriginalGetSign(t *testing.T) {
	p, _ := BuildPluginChannel(epayChannel("https://up.example/", "0", "alipay"))
	params := map[string]string{"pid": "1000", "type": "alipay", "notify_url": "http://shop.example/n", "return_url": "http://shop.example/r", "out_trade_no": "T202610290001", "name": "VIP会员", "money": "1.00", "sign": "ignored", "sign_type": "MD5", "empty": ""}
	// md5("money=1.00&name=VIP会员&notify_url=…&type=alipay" . key), computed independently.
	if got := p.(*epayPlugin).sign(params); got != "08ab1e6b482453c7a3f9dce892b00479" {
		t.Fatal(got)
	}
}

func TestEpayPageModeBuildsEscapedForm(t *testing.T) {
	p, _ := BuildPluginChannel(epayChannel("https://up.example/", "0", "wxpay"))
	if got := p.Capabilities().Methods; len(got) != 1 || got[0] != "h5" {
		t.Fatal(got)
	}
	r := CreateRequest{TradeNo: "T1", Subject: `a"<b>`, Method: "h5", NotifyURL: "https://pay.example/callbacks/providers/9/T1", ReturnURL: "https://pay.example/checkout/T1/return", Amount: 123}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "html" || !strings.Contains(res.Material, `action="https://up.example/submit.php"`) || strings.Contains(res.Material, `<b>`) || !strings.Contains(res.Material, `value="1.23"`) || !strings.Contains(res.Material, `name="sign"`) {
		t.Fatal(res, e)
	}
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "native", NotifyURL: r.NotifyURL, Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("page mode must not offer native", e)
	}
}

func TestEpayMapiModeOutcomes(t *testing.T) {
	var reply string
	var status int
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got, _ = url.ParseQuery(string(b))
		if r.URL.Path != "/mapi.php" || r.Method != http.MethodPost {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, e := BuildPluginChannel(epayChannel(srv.URL+"/", "1", "alipay"))
	if e != nil {
		t.Fatal(e)
	}
	ep := p.(*epayPlugin)
	r := CreateRequest{TradeNo: "T2", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", ClientIP: "192.0.2.1", Amount: 1000}
	status, reply = 200, `{"code":1,"trade_no":"U2","qrcode":"https://qr.example/U2"}`
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.example/U2" || res.ProviderTradeNo != "U2" {
		t.Fatal(res, e)
	}
	signed := map[string]string{}
	for k, v := range got {
		signed[k] = v[0]
	}
	if signed["device"] != "pc" || signed["money"] != "10.00" || signed["clientip"] != "192.0.2.1" || signed["sign"] != ep.sign(signed) || signed["sign_type"] != "MD5" {
		t.Fatal("request", signed)
	}
	r.Method = "wap"
	status, reply = 200, `{"code":1,"trade_no":"U3","payurl":"https://up.example/pay/U3"}`
	if res, e = p.Create(context.Background(), r); e != nil || res.MaterialType != "jump" || got.Get("device") != "mobile" {
		t.Fatal(res, e, got.Get("device"))
	}
	status, reply = 200, `{"code":-1,"msg":"通道维护"}`
	var pe *ProviderError
	if _, e = p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
	var unknown *UnknownOutcome
	for _, c := range []struct {
		status int
		reply  string
	}{{200, `not json`}, {502, `bad gateway`}} {
		status, reply = c.status, c.reply
		if _, e = p.Create(context.Background(), r); !errors.As(e, &unknown) {
			t.Fatal("ambiguous reply must be unknown", c, e)
		}
	}
}

func TestEpayNotificationChecks(t *testing.T) {
	p, _ := BuildPluginChannel(epayChannel("https://up.example/", "0", "alipay"))
	v := p.(NotificationVerifier)
	base := map[string]string{"pid": "1000", "trade_no": "2026102900001", "out_trade_no": "T202610290001", "type": "alipay", "name": "VIP会员", "money": "1.00", "trade_status": "TRADE_SUCCESS", "buyer": "2088xx"}
	encode := func(m map[string]string) []byte {
		q := url.Values{}
		for k, val := range m {
			q.Set(k, val)
		}
		return []byte(q.Encode())
	}
	good := map[string]string{}
	for k, val := range base {
		good[k] = val
	}
	good["sign"] = "e95c9317df69add632633085a0c80ece" // computed independently
	good["sign_type"] = "MD5"
	exp := ExpectedPayment{TradeNo: "T202610290001", Amount: 100}
	ev, e := v.VerifyNotification(context.Background(), nil, encode(good), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "2026102900001" || ev.BuyerID != "2088xx" || ev.Amount != 100 || ev.EventID == "" || ev.Currency != "CNY" {
		t.Fatal(ev, e)
	}
	tamper := func(k, val string) map[string]string {
		m := map[string]string{}
		for kk, vv := range good {
			m[kk] = vv
		}
		m[k] = val
		return m
	}
	for name, m := range map[string]map[string]string{
		"bad sign":     tamper("sign", strings.Repeat("0", 32)),
		"money edited": tamper("money", "100.00"),
	} {
		if _, e = v.VerifyNotification(context.Background(), nil, encode(m), exp); !errors.Is(e, ErrAuthentication) {
			t.Fatal(name, e)
		}
	}
	// Correctly signed but for another order or amount.
	if _, e = v.VerifyNotification(context.Background(), nil, encode(good), ExpectedPayment{TradeNo: "T202610290001", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(good), ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	if _, e = v.VerifyNotification(context.Background(), nil, []byte("a=1&a=2"), exp); e == nil {
		t.Fatal("repeated keys accepted")
	}
	if got := p.(NotificationMethodsProvider).NotificationMethods(); len(got) != 2 || got[0] != http.MethodGet {
		t.Fatal(got)
	}
	if status, _, body := p.(NotificationAcker).NotificationAck(nil); status != 200 || body != "success" {
		t.Fatal(status, body)
	}
}

func TestEpayQueryAndRefund(t *testing.T) {
	var reply string
	var lastForm url.Values
	var lastQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastQuery = r.URL.Query()
		b, _ := io.ReadAll(r.Body)
		lastForm, _ = url.ParseQuery(string(b))
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(epayChannel(srv.URL+"/", "0", "wxpay"))
	reply = `{"code":1,"trade_no":"U9","out_trade_no":"T9","pid":"1000","money":"9.90","status":"1","buyer":"o-x"}`
	q, e := p.Query(context.Background(), QueryRequest{TradeNo: "T9"})
	if e != nil || q.State != "SUCCESS" || q.Amount != 990 || q.ProviderTradeNo != "U9" || lastQuery.Get("act") != "order" || lastQuery.Get("out_trade_no") != "T9" {
		t.Fatal(q, e, lastQuery)
	}
	reply = `{"code":1,"trade_no":"U9","out_trade_no":"T9","pid":"1000","money":"9.90","status":"0"}`
	if q, e = p.Query(context.Background(), QueryRequest{TradeNo: "T9"}); e != nil || q.State != "NOTPAY" {
		t.Fatal(q, e)
	}
	reply = `{"code":1,"trade_no":"U9","out_trade_no":"OTHER","pid":"1000","money":"9.90","status":"1"}`
	if _, e = p.Query(context.Background(), QueryRequest{TradeNo: "T9"}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("foreign order accepted", e)
	}
	reply = `{"code":0,"msg":"退款成功"}`
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T9", ProviderTradeNo: "U9", RefundNo: "R9", Amount: 500, TotalAmount: 990})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 500 || lastQuery.Get("act") != "refund" || lastForm.Get("trade_no") != "U9" || lastForm.Get("money") != "5.00" || lastForm.Get("key") != "synthetic-key" {
		t.Fatal(rr, e, lastForm)
	}
	reply = `{"code":-2,"msg":"商户未开启订单退款API接口"}`
	var pe *ProviderError
	if _, e = p.Refund(context.Background(), RefundRequest{TradeNo: "T9", ProviderTradeNo: "U9", RefundNo: "R9", Amount: 500, TotalAmount: 990}); !errors.As(e, &pe) {
		t.Fatal(e)
	}
}
