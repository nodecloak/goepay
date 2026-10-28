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

func alipayrpChannel() PluginChannel {
	return PluginChannel{ID: 14, Plugin: "alipayrp", PayType: "alipay", Config: map[string]string{
		"appid": "2021app", "appsecret": alipaydTestPriv, "appkey": alipaydTestPub, "appmchid": "2088payee00000001",
	}}
}

func alipayrpWithGateway(t *testing.T, serverURL string) Provider {
	t.Helper()
	c := alipayrpChannel()
	c.HTTPClient = &http.Client{Transport: alipaydRewrite{target: serverURL}}
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func TestPluginAlipayrpRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("alipayrp")
	if !ok || spec.Title != "支付宝现金红包" || !spec.Serves("alipay") {
		t.Fatal("spec", ok)
	}
	if _, e := BuildPluginChannel(alipayrpChannel()); e != nil {
		t.Fatal(e)
	}
	missingKey := alipayrpChannel()
	delete(missingKey.Config, "appkey")
	badSecret := alipayrpChannel()
	badSecret.Config["appsecret"] = "x"
	wrongType := alipayrpChannel()
	wrongType.PayType = "wxpay"
	for i, c := range []PluginChannel{missingKey, badSecret, wrongType} {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginAlipayrpCreateScheme(t *testing.T) {
	p, _ := BuildPluginChannel(alipayrpChannel())
	if m := p.Capabilities().Methods; len(m) != 1 || m[0] != "jsapi" {
		t.Fatal("methods", m)
	}
	// jsapi requires the payer's bound UID in r.OpenID.
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "R1", Subject: "红包", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 500}); !errors.Is(e, ErrInvalid) {
		t.Fatal("missing OpenID must fail", e)
	}
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "R1", Subject: "红包", Method: "jsapi", NotifyURL: "https://pay.example/n", OpenID: "2088payer00000009", Amount: 500})
	if e != nil || res.MaterialType != "urlscheme" {
		t.Fatal(res, e)
	}
	if !strings.HasPrefix(res.Material, "alipays://platformapi/startApp?appId=20000125&orderSuffix=") {
		t.Fatal("scheme", res.Material)
	}
	// The orderSuffix must decode to signed OpenAPI params for app.pay.
	u, _ := url.Parse(strings.SplitN(res.Material, "#", 2)[0])
	inner, _ := url.ParseQuery(u.Query().Get("orderSuffix"))
	if inner.Get("method") != "alipay.fund.trans.app.pay" || inner.Get("sign") == "" || !strings.Contains(inner.Get("biz_content"), "STD_RED_PACKET") {
		t.Fatal("orderSuffix params", inner)
	}
}

func TestPluginAlipayrpQueryAndRefund(t *testing.T) {
	var reply string
	var lastMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f, _ := url.ParseQuery(string(b))
		lastMethod = f.Get("method")
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := alipayrpWithGateway(t, srv.URL)
	signed := func(node, name string) string {
		sig, _ := alipaydSignNodeForTest(node)
		return `{"` + strings.ReplaceAll(name, ".", "_") + `_response":` + node + `,"sign":"` + sig + `"}`
	}

	reply = signed(`{"code":"10000","out_biz_no":"R9","order_id":"F9","trans_amount":"5.00","status":"SUCCESS"}`, "alipay.fund.trans.common.query")
	q, e := p.Query(context.Background(), QueryRequest{TradeNo: "R9"})
	if e != nil || q.State != "SUCCESS" || q.Amount != 500 || q.ProviderTradeNo != "F9" || lastMethod != "alipay.fund.trans.common.query" {
		t.Fatal(q, e)
	}
	// Foreign order rejected.
	reply = signed(`{"code":"10000","out_biz_no":"OTHER","order_id":"F9","trans_amount":"5.00","status":"SUCCESS"}`, "alipay.fund.trans.common.query")
	if _, e = p.Query(context.Background(), QueryRequest{TradeNo: "R9"}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("foreign order accepted", e)
	}

	reply = signed(`{"code":"10000","refund_order_id":"RF9","order_id":"F9","out_request_no":"X9","status":"SUCCESS","refund_amount":"2.00"}`, "alipay.fund.trans.refund")
	rr, e := p.Refund(context.Background(), RefundRequest{ProviderTradeNo: "F9", RefundNo: "X9", Amount: 200, TotalAmount: 500})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 200 || rr.ProviderRefundNo != "RF9" || lastMethod != "alipay.fund.trans.refund" {
		t.Fatal(rr, e)
	}

	// Signed business error -> definitive rejection.
	errNode := `{"code":"40004","sub_code":"AE09xxxx"}`
	esig, _ := alipaydSignNodeForTest(errNode)
	reply = `{"error_response":` + errNode + `,"sign":"` + esig + `"}`
	var pe *ProviderError
	if _, e = p.Refund(context.Background(), RefundRequest{ProviderTradeNo: "F9", RefundNo: "X9", Amount: 200, TotalAmount: 500}); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
}

func TestPluginAlipayrpNotification(t *testing.T) {
	p, _ := BuildPluginChannel(alipayrpChannel())
	v := p.(NotificationVerifier)
	const biz = `{"out_biz_no":"R100","order_id":"FUND2026","trans_amount":"5.00","product_code":"STD_RED_PACKET","biz_scene":"PERSONAL_PAY","status":"SUCCESS"}`
	const notifySig = "ABhxmBNF0lUxKQvV/tM6ZgWYnvqPDCTjohZc50efEFX5p+f1jK842xeKQUj7eNTKoafRLZRae7cafXDR1UpIstagNsDdcRPJdxLm+epgc/qun9OJ7UfKKSnqf/mwUM3CFGsrzAWCu9Rk0CwxyK/PuxYwQZkXUnscyfNZXZMHFxkl2/nV1974/KZnGW8RKSOLFc1iiGXUIqhMRCXiZ6RwKzr5TjNgBRu5gkOunrWRYDL+74QfJTVKU/XsNFxhG2t4UtzYCftBGnr5lP8fJuW1eYo9D1GnKm4lK9AtX6kKYBLgsD9itu0PiWyFzIPX8PFPS6xKBxxinQKbn35r7iYgwQ=="
	params := map[string]string{
		"app_id":      "2021app",
		"msg_method":  "alipay.fund.trans.order.changed",
		"biz_content": biz,
		"charset":     "UTF-8",
		"version":     "1.0",
		"sign_type":   "RSA2",
		"sign":        notifySig,
	}
	encode := func(m map[string]string) []byte {
		q := url.Values{}
		for k, val := range m {
			q.Set(k, val)
		}
		return []byte(q.Encode())
	}
	exp := ExpectedPayment{TradeNo: "R100", Amount: 500}
	ev, e := v.VerifyNotification(context.Background(), nil, encode(params), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "FUND2026" || ev.Amount != 500 {
		t.Fatal(ev, e)
	}
	// Wrong expected amount / order.
	if _, e = v.VerifyNotification(context.Background(), nil, encode(params), ExpectedPayment{TradeNo: "R100", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount expectation", e)
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(params), ExpectedPayment{TradeNo: "OTHER", Amount: 500}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("order expectation", e)
	}
	// Tampered signature.
	bad := map[string]string{}
	for k, val := range params {
		bad[k] = val
	}
	bad["sign"] = strings.Repeat("A", 344)
	if _, e = v.VerifyNotification(context.Background(), nil, encode(bad), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign accepted", e)
	}
	if status, _, body := p.(NotificationAcker).NotificationAck(nil); status != 200 || body != "success" {
		t.Fatal(status, body)
	}
}
