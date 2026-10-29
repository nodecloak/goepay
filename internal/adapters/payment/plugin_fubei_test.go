package payment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func fubeiChannel(payType string, apps ...string) PluginChannel {
	return PluginChannel{ID: 11, Plugin: "fubei", PayType: payType, AppTypes: apps,
		Config: map[string]string{"appid": "APP1", "appkey": "fubei-secret", "appmchid": "S1", "mchid": "M1"}}
}

func TestPluginFubeiRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("fubei")
	if !ok || spec.Title != "付呗聚合支付" || !spec.Serves("alipay") || spec.Serves("qqpay") {
		t.Fatal("fubei spec", ok)
	}
	if _, e := BuildPluginChannel(fubeiChannel("alipay")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{fubeiChannel("qqpay")}
	for _, miss := range []string{"appid", "appkey", "appmchid", "mchid"} {
		c := fubeiChannel("alipay")
		delete(c.Config, miss)
		bad = append(bad, c)
	}
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginFubeiSignatureVector(t *testing.T) {
	p, _ := BuildPluginChannel(fubeiChannel("alipay"))
	fp := p.(*fubeiPlugin)
	biz := `{"merchant_id":"M1","merchant_order_sn":"T1","pay_type":"alipay","total_amount":"1.23","store_id":"S1","user_id":"2088xx","body":"VIP","notify_url":"https://pay.example/n"}`
	common := map[string]string{"app_id": "APP1", "method": "fbpay.order.create", "format": "json", "sign_method": "md5", "nonce": "abc123def456", "version": "1.0", "biz_content": biz}
	// Independently computed with node crypto (upper md5 of ksort(k=v&)+secret).
	if got := fp.sign(common); got != "0E93F5544E8C6480D3BC7AEDB8ED16B3" {
		t.Fatal(got)
	}
}

func TestPluginFubeiCreateJSAPI(t *testing.T) {
	var reply string
	var status int
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := mustFubei(t, srv.URL, "alipay")
	status, reply = 200, `{"result_code":200,"data":{"prepay_id":"PP1","order_sn":"F9"}}`
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 123, OpenID: "2088xx"}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "jsapi" || res.Material != "PP1" || res.ProviderTradeNo != "F9" {
		t.Fatal(res, e)
	}
	// Request field + signature assertions (recompute sign from captured fields).
	captured := map[string]string{}
	for k, v := range got {
		captured[k], _ = v.(string)
	}
	if captured["method"] != "fbpay.order.create" || captured["app_id"] != "APP1" || captured["biz_content"] == "" {
		t.Fatal("request", captured)
	}
	if captured["sign"] != p.(*fubeiPlugin).sign(captured) {
		t.Fatal("sign mismatch", captured["sign"])
	}
	// jsapi requires openid.
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi needs openid", e)
	}
}

func TestPluginFubeiCreateOutcomes(t *testing.T) {
	var reply string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := mustFubei(t, srv.URL, "alipay")
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 123, OpenID: "2088xx"}
	// definitive refusal
	status, reply = 200, `{"result_code":400,"result_message":"store not found"}`
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
	// unknown outcomes
	var unknown *UnknownOutcome
	for _, c := range []struct {
		s int
		b string
	}{{200, "not json"}, {502, "bad gateway"}, {200, `{"result_code":200}`}} {
		status, reply = c.s, c.b
		if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
			t.Fatal("ambiguous reply must be unknown", c, e)
		}
	}
}

func TestPluginFubeiNotification(t *testing.T) {
	p, _ := BuildPluginChannel(fubeiChannel("alipay"))
	v := p.(NotificationVerifier)
	data := `{"order_status":"SUCCESS","merchant_order_sn":"T1","order_sn":"F9","total_amount":"1.23","user_id":"2088xx","channel_order_sn":"C9","ins_order_sn":"I9"}`
	form := func(sign string) []byte {
		b := "data=" + url.QueryEscape(data)
		if sign != "" {
			b += "&sign=" + sign
		}
		return []byte(b)
	}
	// Independently computed callback sign.
	good := form("8F80BF72B5146DE7E7AF71A675AE6A16")
	exp := ExpectedPayment{TradeNo: "T1", Amount: 123}
	ev, e := v.VerifyNotification(context.Background(), nil, good, exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "F9" || ev.Amount != 123 || ev.BuyerID != "2088xx" {
		t.Fatal(ev, e)
	}
	if _, e := v.VerifyNotification(context.Background(), nil, form(strings.Repeat("0", 32)), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign accepted")
	}
	if _, e := v.VerifyNotification(context.Background(), nil, good, ExpectedPayment{TradeNo: "T1", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong amount accepted")
	}
	if _, e := v.VerifyNotification(context.Background(), nil, good, ExpectedPayment{TradeNo: "OTHER", Amount: 123}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong trade accepted")
	}
	badStatus := strings.Replace(data, "SUCCESS", "CLOSED", 1)
	bsForm := []byte("data=" + url.QueryEscape(badStatus) + "&sign=" + fubeiSignFor(p, map[string]string{"data": badStatus}))
	if _, e := v.VerifyNotification(context.Background(), nil, bsForm, exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success accepted", e)
	}
	if s, _, b := p.(NotificationAcker).NotificationAck(nil); s != 200 || b != "success" {
		t.Fatal(s, b)
	}
}

func TestPluginFubeiRefund(t *testing.T) {
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := mustFubei(t, srv.URL, "alipay")
	reply = `{"result_code":200,"data":{"merchant_order_sn":"T1","refund_amount":"1.00"}}`
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "F9", RefundNo: "R1", Amount: 100, TotalAmount: 123})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 100 {
		t.Fatal(rr, e)
	}
	reply = `{"result_code":400,"result_message":"order not found"}`
	var pe *ProviderError
	if _, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "F9", RefundNo: "R1", Amount: 100, TotalAmount: 123}); !errors.As(e, &pe) {
		t.Fatal(e)
	}
}

func mustFubei(t *testing.T, gateway, payType string, apps ...string) Provider {
	t.Helper()
	p, e := BuildPluginChannel(fubeiChannel(payType, apps...))
	if e != nil {
		t.Fatal(e)
	}
	p.(*fubeiPlugin).gateway = gateway
	return p
}

func fubeiSignFor(p Provider, params map[string]string) string {
	return p.(*fubeiPlugin).sign(params)
}
