package payment

import (
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Reuses the shared RSA test keypair and helpers (alipaydTestPub/Priv,
// alipaydSignNodeForTest, alipaydRewrite) declared in plugin_alipayd_test.go.

func alipayslChannel() PluginChannel {
	return PluginChannel{ID: 12, Plugin: "alipaysl", PayType: "alipay", Config: map[string]string{
		"appid": "2021app", "appkey": alipaydTestPub, "appsecret": alipaydTestPriv, "appmchid": "AUTHTOKEN123",
	}}
}

func alipayslWithGateway(t *testing.T, serverURL string) Provider {
	t.Helper()
	c := alipayslChannel()
	c.HTTPClient = &http.Client{Transport: alipaydRewrite{target: serverURL}}
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func TestPluginAlipayslRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("alipaysl")
	if !ok || spec.Title != "支付宝官方支付服务商版" || !spec.Serves("alipay") {
		t.Fatal("alipaysl spec", ok)
	}
	if _, e := BuildPluginChannel(alipayslChannel()); e != nil {
		t.Fatal(e)
	}
	missingToken := alipayslChannel()
	delete(missingToken.Config, "appmchid")
	wrongType := alipayslChannel()
	wrongType.PayType = "qqpay"
	badKey := alipayslChannel()
	badKey.Config["appsecret"] = "bad"
	for i, c := range []PluginChannel{missingToken, wrongType, badKey} {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

// Independent vector: sign content includes app_auth_token; the node-produced
// RSA2 signature verifies in Go.
func TestPluginAlipayslSignatureVector(t *testing.T) {
	const reqContent = `app_auth_token=AUTHTOKEN123&app_id=2021app&biz_content={"business_params":{"mc_create_trade_ip":"192.0.2.9"},"out_trade_no":"S100","product_code":"FAST_INSTANT_TRADE_PAY","subject":"VIP","total_amount":"2.50"}&charset=UTF-8&format=json&method=alipay.trade.page.pay&notify_url=https://pay.example/n&return_url=https://pay.example/r&sign_type=RSA2&timestamp=2026-10-29 10:00:00&version=1.0`
	const reqSig = "fE21Iq3tif5D+P3OtD+LsK6VAwjFW0nh92IOW+bJixmtFwwE9W+RgUkozuW3w2Dfaz0LBELzzTEV8CwMba3VprkJ6YVE3EQB7nSgT7i+Ag8DaxOBbadQDnmciAKMk8ecurzUJ14jLZD3vHVTrdqYhf5ywVx8/EqZHYlb7FX9hUZEMR2ga6VJx8IECXsEfLWJ9AXrqG/chlDdMmL0v0mMv8ppTp4xb9E358usaUc/gZ5CiDIUaaKdAp0pZK4l3F+ZT//IXhc0qoMALE6H0osvAcM93lZad9pQ2b7M/iXh9evoTSS0jNLgGxOdDX/0EscgedEGTfl7wj8w0vsdSBZe+w=="

	biz := map[string]any{
		"out_trade_no":    "S100",
		"total_amount":    "2.50",
		"subject":         "VIP",
		"business_params": map[string]string{"mc_create_trade_ip": "192.0.2.9"},
		"product_code":    "FAST_INSTANT_TRADE_PAY",
	}
	bizJSON, _ := json.Marshal(biz)
	params := map[string]string{
		"app_id": "2021app", "method": "alipay.trade.page.pay", "format": "json",
		"charset": "UTF-8", "sign_type": "RSA2", "version": "1.0", "timestamp": "2026-10-29 10:00:00",
		"app_auth_token": "AUTHTOKEN123", "notify_url": "https://pay.example/n",
		"return_url": "https://pay.example/r", "biz_content": string(bizJSON),
	}
	if got := alipayslSignContent(params); got != reqContent {
		t.Fatalf("sign content mismatch\n got: %s\nwant: %s", got, reqContent)
	}
	pub, _ := pluginPublicKey(alipaydTestPub)
	if !rsaVerify(pub, crypto.SHA256, reqContent, reqSig) {
		t.Fatal("node signature must verify in Go")
	}
}

func TestPluginAlipayslCreateNativeAndForms(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got, _ = url.ParseQuery(string(b))
		node := `{"code":"10000","out_trade_no":"S1","qr_code":"https://qr.alipay.com/s1"}`
		sig, _ := alipaydSignNodeForTest(node)
		io.WriteString(w, `{"alipay_trade_precreate_response":`+node+`,"sign":"`+sig+`"}`)
	}))
	defer srv.Close()
	p := alipayslWithGateway(t, srv.URL)

	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "S1", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.alipay.com/s1" {
		t.Fatal(res, e)
	}
	if got.Get("app_auth_token") != "AUTHTOKEN123" || got.Get("method") != "alipay.trade.precreate" || got.Get("sign") == "" {
		t.Fatal("request", got)
	}

	// page form (no network).
	p2, _ := BuildPluginChannel(alipayslChannel())
	fr, e := p2.Create(context.Background(), CreateRequest{TradeNo: "S2", Subject: "x", Method: "page", NotifyURL: "https://pay.example/n", Amount: 250})
	if e != nil || fr.MaterialType != "html" || !strings.Contains(fr.Material, `name="app_auth_token"`) || !strings.Contains(fr.Material, `name="sign"`) {
		t.Fatal(fr, e)
	}
}

func TestPluginAlipayslOutcomesAndRefund(t *testing.T) {
	var status int
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := alipayslWithGateway(t, srv.URL)

	errNode := `{"code":"40004","sub_code":"ACQ.TRADE_HAS_SUCCESS"}`
	sig, _ := alipaydSignNodeForTest(errNode)
	status, reply = 200, `{"error_response":`+errNode+`,"sign":"`+sig+`"}`
	var pe *ProviderError
	if _, e := p.Refund(context.Background(), RefundRequest{ProviderTradeNo: "U9", RefundNo: "R9", Amount: 100, TotalAmount: 100}); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}

	var unknown *UnknownOutcome
	status, reply = 502, `oops`
	if _, e := p.Query(context.Background(), QueryRequest{TradeNo: "X"}); !errors.As(e, &unknown) {
		t.Fatal("5xx must be unknown", e)
	}

	refNode := `{"code":"10000","trade_no":"U9","out_trade_no":"S9","refund_fee":"1.00"}`
	rsig, _ := alipaydSignNodeForTest(refNode)
	status, reply = 200, `{"alipay_trade_refund_response":`+refNode+`,"sign":"`+rsig+`"}`
	rr, e := p.Refund(context.Background(), RefundRequest{ProviderTradeNo: "U9", RefundNo: "R9", Amount: 100, TotalAmount: 100})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 100 {
		t.Fatal(rr, e)
	}
}

func TestPluginAlipayslNotification(t *testing.T) {
	p, _ := BuildPluginChannel(alipayslChannel())
	v := p.(NotificationVerifier)
	const notifySig = "VJ1nFlR9nijZlf3zk0C+ZiZ2XXPwJ/in3WENdlmKD9aGGkG9rpSXCyrZf8j6o3Q7TZWMlRJeTkbEIs2sCylG4AsvzAe4unuDpCoPTBkCyXxoO2J2Ob9wQFdPxSQx+8j6URXC1yS2jj6HESRi/Ukb/j0FfwQ0ujOBdKgEx8mhwAMeK7p1PG3vSu0uNtJbRQUAqvRZJZ9Vt9oMHZPrQ947ftxhwt760zNL/4NSLiSxLYvXRZcXaOoEcnCOh6TcSheH7yCuJbeJ3Qh/IkvZmjDW1/q4s4oypUFSI+845dv6Zzh54esqiVufouSHSg0GPj3+g52bBWs+Lz8RKi2Q86sQQA=="
	base := map[string]string{
		"app_id": "2021app", "out_trade_no": "S100", "trade_no": "2026777",
		"total_amount": "2.50", "trade_status": "TRADE_SUCCESS", "buyer_id": "2088b", "sign_type": "RSA2",
	}
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
	good["sign"] = notifySig
	exp := ExpectedPayment{TradeNo: "S100", Amount: 250}
	ev, e := v.VerifyNotification(context.Background(), nil, encode(good), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "2026777" || ev.Amount != 250 {
		t.Fatal(ev, e)
	}
	bad := map[string]string{}
	for k, val := range good {
		bad[k] = val
	}
	bad["total_amount"] = "9.99"
	if _, e = v.VerifyNotification(context.Background(), nil, encode(bad), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount tamper accepted", e)
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(good), ExpectedPayment{TradeNo: "OTHER", Amount: 250}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong order accepted", e)
	}
	if status, _, body := p.(NotificationAcker).NotificationAck(nil); status != 200 || body != "success" {
		t.Fatal(status, body)
	}
}
