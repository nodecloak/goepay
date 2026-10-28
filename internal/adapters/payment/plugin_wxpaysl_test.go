package payment

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func wxpayslChannel(appTypes []string) PluginChannel {
	return PluginChannel{
		ID:       21,
		Plugin:   "wxpaysl",
		PayType:  "wxpay",
		AppTypes: appTypes,
		Config: map[string]string{
			"appid":    "wx8888",
			"appmchid": "1900000109",
			"appkey":   "synthetic-apiv2-key",
			"appurl":   "1900000110",
		},
	}
}

func TestPluginWxpayslRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("wxpaysl")
	if !ok || spec.Title != "微信官方支付服务商版" || !spec.Serves("wxpay") || spec.Serves("alipay") {
		t.Fatal("wxpaysl spec", spec, ok)
	}
	if _, e := BuildPluginChannel(wxpayslChannel([]string{"1"})); e != nil {
		t.Fatal(e)
	}
	// Missing required appurl (sub_mch_id).
	missing := wxpayslChannel([]string{"1"})
	delete(missing.Config, "appurl")
	// A literal sub_mchid key is unknown input -> rejected (and would also trip
	// channelconfig). We keep the original "appurl" key instead.
	unknown := wxpayslChannel([]string{"1"})
	unknown.Config["sub_mchid"] = "x"
	bad := []PluginChannel{
		wxpayslChannel([]string{"9"}), // apptype not offered
		missing,
		unknown,
	}
	alipayType := wxpayslChannel([]string{"1"})
	alipayType.PayType = "alipay"
	bad = append(bad, alipayType)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginWxpayslSignatureMatchesSDK(t *testing.T) {
	p, _ := BuildPluginChannel(wxpayslChannel([]string{"1"}))
	wp := p.(*wxpayslPlugin)
	params := map[string]string{
		"appid": "wx8888", "mch_id": "1900000109", "sub_mch_id": "1900000110",
		"nonce_str": "abc123", "body": "商品", "out_trade_no": "T202610290001",
		"total_fee": "100", "spbill_create_ip": "127.0.0.1", "trade_type": "NATIVE",
		"product_id": "01001", "notify_url": "https://pay.example/n", "sign_type": "MD5",
	}
	// Independently computed with node crypto (see p05.md):
	//   keys sorted, non-empty "k=v" joined by "&", then "&key=<apikey>", md5, upper.
	if got := wp.sign(params); got != "6483511CD41CEF74385B8DB73A356942" {
		t.Fatal("v2 sign vector mismatch:", got)
	}
	// Self-consistency: VerifyWechatV2 accepts our own signature.
	sig := wp.sign(params)
	params["sign"] = sig
	if !VerifyWechatV2(params, "synthetic-apiv2-key", "MD5", sig) {
		t.Fatal("v2 sign not self-verifying")
	}
}

func TestPluginWxpayslMethods(t *testing.T) {
	p, _ := BuildPluginChannel(wxpayslChannel([]string{"1", "2", "3", "5"}))
	m := p.Capabilities().Methods
	want := map[string]bool{"native": true, "h5": true, "app": true, "jsapi": true}
	if len(m) != 4 {
		t.Fatal(m)
	}
	for _, x := range m {
		if !want[x] {
			t.Fatal("unexpected method", x)
		}
	}
	if !p.Capabilities().Query || !p.Capabilities().Close || p.Capabilities().Refund {
		t.Fatal("caps", p.Capabilities())
	}
}

func wxpayslXML(m map[string]string) string {
	var b strings.Builder
	b.WriteString("<xml>")
	for k, v := range m {
		b.WriteString("<" + k + "><![CDATA[" + v + "]]></" + k + ">")
	}
	b.WriteString("</xml>")
	return b.String()
}

func TestPluginWxpayslCreateNative(t *testing.T) {
	var gotBody string
	var reply string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		if r.URL.Path != "/pay/unifiedorder" || r.Method != http.MethodPost {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(wxpayslChannel([]string{"1"}))
	wp := p.(*wxpayslPlugin)
	// Verify the request the plugin builds, then feed a signed reply.
	status = 200
	reply = wxpayslXML(map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "code_url": "weixin://wxpay/bizpayurl?pr=abc", "prepay_id": "wx201610"})
	r := CreateRequest{TradeNo: "T1", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100, ClientIP: "192.0.2.1"}
	params := wp.publicParams()
	params["body"] = r.Subject
	params["out_trade_no"] = r.TradeNo
	params["total_fee"] = "100"
	params["spbill_create_ip"] = "192.0.2.1"
	params["notify_url"] = r.NotifyURL
	params["trade_type"] = "NATIVE"
	params["product_id"] = "01001"
	res, e := wp.execute(context.Background(), "wxpaysl.unifiedorder", srv.URL+"/pay/unifiedorder", params, false)
	if e != nil {
		t.Fatal(e)
	}
	if res["code_url"] != "weixin://wxpay/bizpayurl?pr=abc" {
		t.Fatal(res)
	}
	// The signed XML request carries sign and the service-provider fields.
	var doc struct {
		XMLName  xml.Name
		Children []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		} `xml:",any"`
	}
	if e := xml.Unmarshal([]byte(gotBody), &doc); e != nil {
		t.Fatal("request not XML", e, gotBody)
	}
	fields := map[string]string{}
	for _, c := range doc.Children {
		fields[c.XMLName.Local] = c.Value
	}
	if fields["sub_mch_id"] != "1900000110" || fields["mch_id"] != "1900000109" || fields["sign"] == "" || fields["trade_type"] != "NATIVE" {
		t.Fatal("request fields", fields)
	}
	if !VerifyWechatV2(fields, "synthetic-apiv2-key", "MD5", fields["sign"]) {
		t.Fatal("request sign invalid")
	}

	// Business failure is definitive.
	reply = wxpayslXML(map[string]string{"return_code": "SUCCESS", "result_code": "FAIL", "err_code": "ORDERPAID"})
	var pe *ProviderError
	if _, e := wp.execute(context.Background(), "op", srv.URL+"/pay/unifiedorder", wp.publicParams(), false); !errors.As(e, &pe) {
		t.Fatal("business fail not definitive", e)
	}
	// 5xx is unknown.
	status = 502
	reply = "bad"
	var uo *UnknownOutcome
	if _, e := wp.execute(context.Background(), "op", srv.URL+"/pay/unifiedorder", wp.publicParams(), false); !errors.As(e, &uo) {
		t.Fatal("5xx not unknown", e)
	}
	_ = res
}

func TestPluginWxpayslNotification(t *testing.T) {
	p, _ := BuildPluginChannel(wxpayslChannel([]string{"1"}))
	wp := p.(*wxpayslPlugin)
	data := map[string]string{
		"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "wx8888",
		"mch_id": "1900000109", "sub_mch_id": "1900000110", "out_trade_no": "T1",
		"transaction_id": "42000", "total_fee": "100", "openid": "oABC",
	}
	data["sign"] = wp.sign(data)
	raw := []byte(wxpayslXML(data))
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := wp.VerifyNotification(context.Background(), nil, raw, exp)
	if e != nil || ev.State != "SUCCESS" || ev.Amount != 100 || ev.ProviderTradeNo != "42000" || ev.BuyerID != "oABC" {
		t.Fatal(ev, e)
	}
	// Bad sign.
	bad := map[string]string{}
	for k, v := range data {
		bad[k] = v
	}
	bad["sign"] = strings.Repeat("0", 32)
	if _, e := wp.VerifyNotification(context.Background(), nil, []byte(wxpayslXML(bad)), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign accepted", e)
	}
	// Wrong amount / wrong trade no.
	if _, e := wp.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount")
	}
	if _, e := wp.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade")
	}
	// Non-success result_code.
	ns := map[string]string{}
	for k, v := range data {
		ns[k] = v
	}
	ns["result_code"] = "FAIL"
	ns["sign"] = wp.sign(ns)
	if _, e := wp.VerifyNotification(context.Background(), nil, []byte(wxpayslXML(ns)), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success accepted", e)
	}
	// Ack bodies.
	st, ct, body := wp.NotificationAck(nil)
	if st != 200 || !strings.Contains(body, "SUCCESS") || !strings.Contains(ct, "xml") {
		t.Fatal("ack ok", st, body)
	}
	if _, _, fb := wp.NotificationAck(errors.New("x")); !strings.Contains(fb, "FAIL") {
		t.Fatal("ack fail", fb)
	}
}

func TestPluginWxpayslQuery(t *testing.T) {
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(wxpayslChannel([]string{"1"}))
	wp := p.(*wxpayslPlugin)
	q := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "out_trade_no": "T1", "transaction_id": "42000", "total_fee": "100", "trade_state": "SUCCESS", "openid": "oABC"}
	q["sign"] = wp.sign(q)
	reply = wxpayslXML(q)
	params := wp.publicParams()
	params["out_trade_no"] = "T1"
	res, e := wp.execute(context.Background(), "wxpaysl.orderquery", srv.URL+"/pay/orderquery", params, false)
	if e != nil || res["trade_state"] != "SUCCESS" {
		t.Fatal(res, e)
	}
}
