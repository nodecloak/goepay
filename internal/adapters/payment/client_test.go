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
	"sync/atomic"
	"testing"
	"time"
)

func TestWechatClientAuthenticatesResponsesAndCallbacks(t *testing.T) {
	key := "test-only-api-key-32-bytes-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		p, e := decodeXML(raw)
		if e != nil || !VerifyWechatV2(p, key, "MD5", p["sign"]) {
			t.Error("request signature invalid")
		}
		if r.URL.Path != "/pay/unifiedorder" || p["total_fee"] != "101" || p["trade_type"] != "NATIVE" {
			t.Errorf("wrong request %s %v", r.URL.Path, p)
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch", "code_url": "weixin://test"}
		out["sign"], _ = SignWechatV2(out, key, "MD5")
		w.Write(encodeXML(out))
	}))
	defer server.Close()
	provider, e := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: key, BaseURL: server.URL, HTTPClient: server.Client()})
	if e != nil {
		t.Fatal(e)
	}
	result, e := provider.Create(context.Background(), CreateRequest{TradeNo: "t1", Subject: "中文商品", Method: "native", Amount: 101, ClientIP: "192.0.2.1", NotifyURL: "https://merchant.invalid/hook"})
	if e != nil || result.Material != "weixin://test" {
		t.Fatalf("create %v %v", result, e)
	}
	p := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch", "out_trade_no": "t1", "transaction_id": "wx1", "total_fee": "101"}
	p["sign"], _ = SignWechatV2(p, key, "MD5")
	expected := ExpectedPayment{TradeNo: "t1", AppID: "app", MerchantID: "mch", Amount: 101}
	if _, e := provider.VerifyNotification(context.Background(), nil, encodeXML(p), expected); e != nil {
		t.Fatal(e)
	}
	expected.Amount = 102
	if _, e := provider.VerifyNotification(context.Background(), nil, encodeXML(p), expected); e == nil {
		t.Fatal("amount mismatch accepted")
	}
	if provider.Capabilities().Refund {
		t.Fatal("refund enabled without mTLS client")
	}
}
func TestWechatUnverifiedSuccessIsUnknown(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<xml><return_code>SUCCESS</return_code><result_code>SUCCESS</result_code><code_url>weixin://forged</code_url></xml>`))
	}))
	defer upstream.Close()
	provider, _ := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: "test-only-long-api-key", BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	_, e := provider.Create(context.Background(), CreateRequest{TradeNo: "t1", Subject: "test", Method: "native", Amount: 1, NotifyURL: "https://example.invalid/hook", ClientIP: "192.0.2.1"})
	var unknown *UnknownOutcome
	if !errors.As(e, &unknown) {
		t.Fatalf("unverified write response should require query: %v", e)
	}
}
func TestAlipayPageAndQuery(t *testing.T) {
	merchant := testKey(t)
	platform := testKey(t)
	var requested bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
		r.ParseForm()
		p := map[string]string{}
		for k, v := range r.Form {
			if len(v) != 1 {
				t.Error("duplicate field")
			}
			p[k] = v[0]
		}
		if !verifyRSA([]byte(AlipayRequestCanonical(p)), p["sign"], &merchant.PublicKey) {
			t.Error("bad Alipay API request signature")
		}
		raw := []byte(`{"code":"10000","out_trade_no":"t1","trade_no":"ali1","trade_status":"TRADE_SUCCESS","total_amount":"1.01","buyer_user_id":"buyer"}`)
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_query_response": json.RawMessage(raw), "sign": independentSignature(t, string(raw), platform)})
	}))
	defer upstream.Close()
	provider, e := NewAlipay(AlipayConfig{AppID: "app", SellerID: "seller", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	result, e := provider.Create(context.Background(), CreateRequest{TradeNo: "t1", Subject: "<商品>&\"", Method: "page", Amount: 101, NotifyURL: "https://example.invalid/hook", ReturnURL: "https://example.invalid/return"})
	if e != nil || result.MaterialType != "html" || strings.Contains(result.Material, "<商品>") || !strings.Contains(result.Material, "&#34;") {
		t.Fatalf("page %v %v", result, e)
	}
	if requested {
		t.Fatal("page signing invoked upstream")
	}
	query, e := provider.Query(context.Background(), QueryRequest{TradeNo: "t1"})
	if e != nil || query.Amount != 101 || query.ProviderTradeNo != "ali1" {
		t.Fatalf("query %v %v", query, e)
	}
	p := map[string]string{"app_id": "app", "seller_id": "seller", "out_trade_no": "t1", "trade_no": "ali1", "trade_status": "TRADE_SUCCESS", "total_amount": "1.01", "sign_type": "RSA2"}
	p["sign"] = independentSignature(t, AlipayNotificationCanonical(p), platform)
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	expected := ExpectedPayment{AppID: "app", MerchantID: "seller", TradeNo: "t1", Amount: 101}
	if event, e := provider.VerifyNotification(context.Background(), nil, []byte(v.Encode()), expected); e != nil || event.State != "SUCCESS" {
		t.Fatal(e)
	}
	v.Set("seller_id", "wrong")
	p["seller_id"] = "wrong"
	v.Set("sign", independentSignature(t, AlipayNotificationCanonical(p), platform))
	if _, e := provider.VerifyNotification(context.Background(), nil, []byte(v.Encode()), expected); e == nil {
		t.Fatal("wrong seller accepted despite valid signature")
	}
}

func TestWechatJSAPIMaterialsAndStrictXML(t *testing.T) {
	provider, _ := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: "test-only-api-key-32-bytes-secret"})
	p, e := provider.JSAPIParams("prepay1", time.Unix(1700000000, 0))
	if e != nil {
		t.Fatal(e)
	}
	sig := p["paySign"]
	delete(p, "paySign")
	if p["appId"] != "app" || p["package"] != "prepay_id=prepay1" || p["timeStamp"] != "1700000000" || !VerifyWechatV2(p, provider.config.APIKey, "MD5", sig) {
		t.Fatal("bad JSAPI material")
	}
	for _, raw := range []string{`<xml><a>1</a><a>2</a></xml>`, `<xml><a><nested>1</nested></a></xml>`, `<!DOCTYPE xml><xml><a>1</a></xml>`, `<xml><a>1</a></xml><xml></xml>`} {
		if _, e := decodeXML([]byte(raw)); e == nil {
			t.Errorf("ambiguous XML accepted %q", raw)
		}
	}
}
func TestWechatQueryCloseRefund(t *testing.T) {
	key := "test-only-api-key-32-bytes-secret"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		p, e := decodeXML(raw)
		if e != nil || !VerifyWechatV2(p, key, "MD5", p["sign"]) {
			t.Error("request not signed")
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch", "out_trade_no": "t1"}
		switch r.URL.Path {
		case "/pay/orderquery":
			out["trade_state"] = "SUCCESS"
			out["transaction_id"] = "wx1"
			out["total_fee"] = "101"
		case "/secapi/pay/refund":
			if p["total_fee"] != "101" || p["refund_fee"] != "1" || p["out_refund_no"] != "r1" {
				t.Error("wrong refund request")
			}
			out["out_refund_no"] = "r1"
			out["refund_id"] = "wxr1"
			out["refund_fee"] = "1"
		case "/pay/refundquery":
			out["total_fee"] = "101"
			out["out_refund_no_0"] = "r1"
			out["refund_id_0"] = "wxr1"
			out["refund_status_0"] = "SUCCESS"
			out["refund_fee_0"] = "1"
		case "/pay/closeorder":
		default:
			t.Error("unexpected path")
		}
		out["sign"], _ = SignWechatV2(out, key, "MD5")
		w.Write(encodeXML(out))
	}))
	defer upstream.Close()
	provider, _ := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: key, BaseURL: upstream.URL, HTTPClient: upstream.Client(), RefundClient: upstream.Client()})
	q, e := provider.Query(context.Background(), QueryRequest{TradeNo: "t1"})
	if e != nil || q.Amount != 101 || q.State != "SUCCESS" {
		t.Fatalf("query %v %v", q, e)
	}
	if e := provider.Close(context.Background(), QueryRequest{TradeNo: "t1"}); e != nil {
		t.Fatal(e)
	}
	r := RefundRequest{TradeNo: "t1", RefundNo: "r1", TotalAmount: 101, Amount: 1}
	refund, e := provider.Refund(context.Background(), r)
	if e != nil || refund.State != "PROCESSING" {
		t.Fatalf("refund %v %v", refund, e)
	}
	refund, e = provider.QueryRefund(context.Background(), r)
	if e != nil || refund.State != "SUCCESS" || refund.Amount != 1 {
		t.Fatalf("refundquery %v %v", refund, e)
	}
}
func TestWechatMissingMaterialPreservesUnknownOutcome(t *testing.T) {
	key := "test-only-api-key-32-bytes-secret"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch"}
		out["sign"], _ = SignWechatV2(out, key, "MD5")
		w.Write(encodeXML(out))
	}))
	defer upstream.Close()
	provider, _ := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: key, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	_, e := provider.Create(context.Background(), CreateRequest{TradeNo: "t1", Subject: "test", Method: "native", Amount: 1, NotifyURL: "https://example.invalid/hook", ClientIP: "192.0.2.1"})
	var unknown *UnknownOutcome
	if !errors.As(e, &unknown) {
		t.Fatalf("missing material after write must remain unknown: %v", e)
	}
}
func TestAlipayWrongResponseBytesAndWriteFailures(t *testing.T) {
	merchant := testKey(t)
	platform := testKey(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node := []byte(`{"code":"10000","out_trade_no":"t1"}`)
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_close_response": json.RawMessage(node), "sign": independentSignature(t, string(node)+" ", platform)})
	}))
	defer upstream.Close()
	provider, _ := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	e := provider.Close(context.Background(), QueryRequest{TradeNo: "t1"})
	var unknown *UnknownOutcome
	if !errors.As(e, &unknown) {
		t.Fatalf("unverified close must be unknown: %v", e)
	}
}
func TestClientsNeverFollowUpstreamRedirects(t *testing.T) {
	followed := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed = true; w.WriteHeader(200) }))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()
	wx, _ := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: "test-only-api-key-32-bytes-secret", BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	wx.Close(context.Background(), QueryRequest{TradeNo: "t1"})
	key := testKey(t)
	ali, _ := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	ali.Close(context.Background(), QueryRequest{TradeNo: "t1"})
	if followed {
		t.Fatal("provider followed redirect, leaking signed request")
	}
}
func TestAlipayRefundAndQueryRefund(t *testing.T) {
	merchant := testKey(t)
	platform := testKey(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		method := r.Form.Get("method")
		p := map[string]string{}
		for k, vs := range r.Form {
			p[k] = vs[0]
		}
		if !verifyRSA([]byte(AlipayRequestCanonical(p)), p["sign"], &merchant.PublicKey) {
			t.Error("invalid request sign")
			w.WriteHeader(400)
			return
		}
		var biz map[string]any
		json.Unmarshal([]byte(p["biz_content"]), &biz)
		if biz["out_request_no"] != "refund-stable" || biz["out_trade_no"] != "t1" {
			t.Error("missing stable business identifiers")
		}
		var node string
		if method == "alipay.trade.refund" {
			if biz["refund_amount"] != "0.01" {
				t.Error("money not exact")
			}
			node = `{"code":"10000","out_trade_no":"t1","trade_no":"ali1","refund_fee":"0.01","fund_change":"Y"}`
		} else {
			node = `{"code":"10000","out_trade_no":"t1","trade_no":"ali1","out_request_no":"refund-stable","refund_amount":"0.01","refund_status":"REFUND_SUCCESS"}`
		}
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(method, ".", "_") + "_response": json.RawMessage(node), "sign": independentSignature(t, node, platform)})
	}))
	defer upstream.Close()
	a, _ := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	request := RefundRequest{TradeNo: "t1", RefundNo: "refund-stable", TotalAmount: 101, Amount: 1}
	v, e := a.Refund(context.Background(), request)
	if e != nil || v.Amount != 1 || v.State != "SUCCESS" {
		t.Fatalf("refund %v %v", v, e)
	}
	v, e = a.QueryRefund(context.Background(), request)
	if e != nil || v.State != "REFUND_SUCCESS" || v.Amount != 1 {
		t.Fatalf("refundquery %v %v", v, e)
	}
}
func TestCapabilitiesNeverAdvertiseUnimplementedProducts(t *testing.T) {
	if _, e := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: "test-only-api-key-32-bytes-secret", Methods: []string{"face"}}); e == nil {
		t.Fatal("unimplemented WeChat face-payment product advertised")
	}
	key := testKey(t)
	if _, e := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"transfer"}}); e == nil {
		t.Fatal("unknown product advertised")
	}
}
func TestAlipayNativeCarriesTopLevelNotifyURL(t *testing.T) {
	merchant := testKey(t)
	platform := testKey(t)
	const notify = "https://merchant.example.invalid/callback"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("notify_url") != notify {
			t.Error("native precreate lost top-level notify_url")
		}
		var biz map[string]any
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
		if _, exists := biz["notify_url"]; exists {
			t.Error("notify_url put inside business JSON")
		}
		node := `{"code":"10000","out_trade_no":"t1","qr_code":"https://qr.alipay.test/signed"}`
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_precreate_response": json.RawMessage(node), "sign": independentSignature(t, node, platform)})
	}))
	defer upstream.Close()
	provider, e := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: upstream.URL, HTTPClient: upstream.Client(), Methods: []string{"native"}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = provider.Create(context.Background(), CreateRequest{TradeNo: "t1", Subject: "test", Amount: 101, Method: "native", NotifyURL: notify})
	if e != nil {
		t.Fatal(e)
	}
}
func TestWechatRefundQueryBindsOrderAndOriginalTotal(t *testing.T) {
	key := "test-only-api-key-32-bytes-secret"
	var order, total atomic.Value
	order.Store("wrong-order")
	total.Store("100")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch", "out_trade_no": order.Load().(string), "total_fee": total.Load().(string), "out_refund_no_0": "RF123", "refund_fee_0": "50", "refund_status_0": "SUCCESS", "refund_id_0": "upstream-r1"}
		p["sign"], _ = SignWechatV2(p, key, "MD5")
		w.Write(encodeXML(p))
	}))
	defer upstream.Close()
	p, _ := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: key, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	request := RefundRequest{TradeNo: "correct-order", RefundNo: "RF123", TotalAmount: 100, Amount: 50}
	if _, e := p.QueryRefund(context.Background(), request); e == nil {
		t.Fatal("validly signed refund for other order accepted")
	}
	order.Store("correct-order")
	total.Store("101")
	if _, e := p.QueryRefund(context.Background(), request); e == nil {
		t.Fatal("wrong original order amount accepted")
	}
	total.Store("100")
	if _, e := p.QueryRefund(context.Background(), request); e != nil {
		t.Fatal(e)
	}
}
