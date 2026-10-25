package payment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAlipayPreauthFreezeProducesSignedAuthorizationMaterialOnly(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	p, e := NewAlipay(AlipayConfig{AppID: "app", SellerID: "seller", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}})
	if e != nil {
		t.Fatalf("preauthorization capability unavailable: %v", e)
	}
	result, e := p.Create(context.Background(), CreateRequest{TradeNo: "freeze-1", Subject: "deposit", Method: "preauth", Amount: 101, NotifyURL: "https://pay.example.invalid/pay/preauthnotify/freeze-1/", ClientIP: "192.0.2.1"})
	if e != nil {
		t.Fatal(e)
	}
	if result.MaterialType != "preauth" || result.ProviderTradeNo != "" {
		t.Fatalf("freeze is not payment: %+v", result)
	}
	v, e := url.ParseQuery(result.Material)
	if e != nil {
		t.Fatal(e)
	}
	params := map[string]string{}
	for k, values := range v {
		params[k] = values[0]
	}
	if params["method"] != "alipay.fund.auth.order.app.freeze" || !verifyRSA([]byte(AlipayRequestCanonical(params)), params["sign"], &merchant.PublicKey) {
		t.Fatal("invalid freeze request signature", params)
	}
	var biz map[string]any
	json.Unmarshal([]byte(params["biz_content"]), &biz)
	if biz["out_order_no"] != "freeze-1" || biz["out_request_no"] != "freeze-1" || biz["amount"] != "1.01" || biz["product_code"] != "PREAUTH_PAY" || biz["payee_user_id"] != "seller" || biz["total_amount"] != nil || biz["out_trade_no"] != nil {
		t.Fatal(biz)
	}
}

func TestAlipayPreauthNotificationRejectsForgedAmountIdentityAndOperation(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	p, e := NewAlipay(AlipayConfig{AppID: "app", SellerID: "seller", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}})
	if e != nil {
		t.Fatal(e)
	}
	base := map[string]string{"app_id": "app", "payee_user_id": "seller", "out_order_no": "freeze-1", "out_request_no": "freeze-1", "amount": "1.01", "auth_no": "authorization-42", "payer_user_id": "2088000000000000", "status": "SUCCESS", "operation_type": "FREEZE", "sign_type": "RSA2"}
	encode := func(p map[string]string) []byte {
		p["sign"] = independentSignature(t, AlipayNotificationCanonical(p), platform)
		v := url.Values{}
		for k, val := range p {
			v.Set(k, val)
		}
		return []byte(v.Encode())
	}
	event, e := p.VerifyAuthorization(context.Background(), nil, encode(base), ExpectedPayment{TradeNo: "freeze-1", Amount: 101})
	if e != nil || event.AuthNo != "authorization-42" || event.Amount != 101 || event.State != "SUCCESS" {
		t.Fatal(event, e)
	}
	for _, tt := range []struct{ key, value string }{{"amount", "1.02"}, {"app_id", "wrong"}, {"payee_user_id", "wrong"}, {"out_request_no", "wrong"}, {"auth_no", "@not-signed"}, {"payer_user_id", "@not-signed"}, {"operation_type", "UNFREEZE"}, {"status", "INIT"}} {
		altered := map[string]string{}
		for k, v := range base {
			altered[k] = v
		}
		altered[tt.key] = tt.value
		if _, e := p.VerifyAuthorization(context.Background(), nil, encode(altered), ExpectedPayment{TradeNo: "freeze-1", Amount: 101}); !errors.Is(e, ErrAuthentication) {
			t.Fatal("authorization mismatch accepted", tt, e)
		}
	}
	if _, e := p.VerifyAuthorization(context.Background(), nil, append(encode(base), []byte("&amount=1.01")...), ExpectedPayment{TradeNo: "freeze-1", Amount: 101}); e == nil {
		t.Fatal("duplicate signed field accepted")
	}
}

func TestAlipayPreauthCaptureAuthenticatesPaymentBeforeSuccess(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	bad := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var biz map[string]any
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
		if r.Form.Get("method") != "alipay.trade.pay" || biz["auth_no"] != "authorization-42" || biz["auth_confirm_mode"] != "COMPLETE" || biz["product_code"] != "PREAUTH_PAY" || biz["total_amount"] != "1.01" || biz["out_trade_no"] != "freeze-1" || r.Form.Get("notify_url") == "" {
			t.Error(biz, r.Form)
		}
		node := map[string]any{"code": "10000", "out_trade_no": "freeze-1", "trade_no": "payment-99", "total_amount": "1.01", "buyer_user_id": "2088000000000000"}
		if bad {
			node["total_amount"] = "1.02"
		}
		raw, _ := json.Marshal(node)
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_pay_response": json.RawMessage(raw), "sign": independentSignature(t, string(raw), platform)})
	}))
	defer upstream.Close()
	p, e := NewAlipay(AlipayConfig{AppID: "app", SellerID: "seller", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	request := AuthorizationCaptureRequest{TradeNo: "freeze-1", AuthNo: "authorization-42", BuyerID: "2088000000000000", Subject: "deposit", Amount: 101, NotifyURL: "https://pay.example.invalid/notify"}
	result, e := p.CaptureAuthorization(context.Background(), request)
	if e != nil || result.ProviderTradeNo != "payment-99" || result.Currency != "CNY" || result.Amount != 101 {
		t.Fatal(result, e)
	}
	bad = true
	_, e = p.CaptureAuthorization(context.Background(), request)
	var unknown *UnknownOutcome
	if !errors.As(e, &unknown) {
		t.Fatal("untrusted capture did not remain unknown", e)
	}
}

func TestAlipayPreauthQueryAndReleaseProtocolsBindOriginalAuthorization(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	bad := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var biz map[string]any
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
		method := r.Form.Get("method")
		node := map[string]any{"code": "10000", "auth_no": "authorization-42", "out_order_no": "freeze-1", "out_request_no": "release-1", "amount": "1.01", "rest_amount": "0.00", "status": "SUCCESS", "order_status": "AUTHORIZED", "payer_user_id": "2088000000000000"}
		switch method {
		case "alipay.fund.auth.operation.detail.query":
			if biz["out_order_no"] != "freeze-1" || biz["out_request_no"] != "release-1" {
				t.Error(biz)
			}
		case "alipay.fund.auth.order.unfreeze":
			if biz["auth_no"] != "authorization-42" || biz["amount"] != "1.01" || biz["out_request_no"] != "release-1" {
				t.Error(biz)
			}
		case "alipay.fund.auth.operation.cancel":
			if biz["auth_no"] != "authorization-42" || biz["out_order_no"] != "freeze-1" || biz["out_request_no"] != "release-1" {
				t.Error(biz)
			}
		default:
			t.Error("unexpected preauth operation", method)
		}
		if bad {
			node["auth_no"] = "different-authorization"
		}
		raw, _ := json.Marshal(node)
		field := ""
		for _, c := range method {
			if c == '.' {
				field += "_"
			} else {
				field += string(c)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{field + "_response": json.RawMessage(raw), "sign": independentSignature(t, string(raw), platform)})
	}))
	defer upstream.Close()
	p, e := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	request := AuthorizationRequest{TradeNo: "freeze-1", RequestNo: "release-1", AuthNo: "authorization-42", Amount: 101}
	for _, operation := range []func(context.Context, AuthorizationRequest) (AuthorizationEvent, error){p.QueryAuthorization, p.UnfreezeAuthorization, p.CancelAuthorization} {
		result, e := operation(context.Background(), request)
		if e != nil || result.AuthNo != "authorization-42" {
			t.Fatal(result, e)
		}
	}
	bad = true
	if _, e = p.QueryAuthorization(context.Background(), request); !errors.Is(e, ErrAuthentication) {
		t.Fatal("query accepted other authorization", e)
	}
	for _, operation := range []func(context.Context, AuthorizationRequest) (AuthorizationEvent, error){p.UnfreezeAuthorization, p.CancelAuthorization} {
		_, e := operation(context.Background(), request)
		var unknown *UnknownOutcome
		if !errors.As(e, &unknown) {
			t.Fatal("unverified release treated as definitive", e)
		}
	}
}
