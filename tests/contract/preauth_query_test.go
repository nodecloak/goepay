package contract_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPreauthQueryCanRecoverAuthorizationWithoutPaymentTrade(t *testing.T) {
	s, _, merchant := fixture(t)
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	mode := "authorized"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		method := r.Form.Get("method")
		var biz map[string]any
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
		var node map[string]any
		field := ""
		switch method {
		case "alipay.fund.auth.operation.detail.query":
			if mode == "auth-unknown-payment-success" {
				w.WriteHeader(503)
				return
			}
			field = "alipay_fund_auth_operation_detail_query_response"
			node = map[string]any{"code": "10000", "out_order_no": biz["out_order_no"], "out_request_no": biz["out_request_no"], "auth_no": "authorization-query", "payer_user_id": "2088000000000000", "amount": "1.25", "rest_amount": "1.25", "status": "SUCCESS", "order_status": "AUTHORIZED"}
		case "alipay.trade.query":
			field = "alipay_trade_query_response"
			if mode == "auth-unknown-payment-success" {
				node = map[string]any{"code": "10000", "out_trade_no": biz["out_trade_no"], "trade_no": "payment-query", "trade_status": "TRADE_SUCCESS", "total_amount": "1.25"}
			} else {
				node = map[string]any{"code": "40004", "sub_code": "ACQ.TRADE_NOT_EXIST"}
				if mode == "other-rejection" {
					node["sub_code"] = "ACQ.SYSTEM_ERROR"
				}
			}
		default:
			t.Error("query issued a write", method)
			w.WriteHeader(500)
			return
		}
		raw, _ := json.Marshal(node)
		digest := sha256.Sum256(raw)
		signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		sig := base64.StdEncoding.EncodeToString(signature)
		if mode == "bad-signature" {
			sig = "invalid"
		}
		json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(raw), "sign": sig})
	}))
	defer upstream.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	g := payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	ctx := context.Background()
	order, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "no-capture-trade", Name: "deposit", Type: "alipay", Amount: 125})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.SubmitPreauthorization(ctx, order.TradeNo, 7, "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	g.AllowLive = false
	result, e := g.Query(ctx, 2, order.TradeNo)
	if e != nil || result.State != "AUTHORIZED" || result.Amount != 125 || result.ProviderTradeNo != "" {
		t.Fatal("authorized funds incorrectly require a payment trade", result, e)
	}
	auth, e := s.Store.Preauthorization(ctx, order.TradeNo)
	balance, _ := s.Store.Balance(ctx, 2)
	if e != nil || auth.CaptureState != "none" || balance.Available != 0 {
		t.Fatal(auth, balance, e)
	}
	mode = "bad-signature"
	if _, e = g.Query(ctx, 2, order.TradeNo); !errors.Is(e, adapter.ErrAuthentication) {
		t.Fatal("unverified notfound suppressed", e)
	}
	mode = "other-rejection"
	if _, e = g.Query(ctx, 2, order.TradeNo); e == nil {
		t.Fatal("arbitrary upstream error suppressed")
	}
	second, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "auth-query-unavailable", Name: "deposit", Type: "alipay", Amount: 125})
	if e != nil {
		t.Fatal(e)
	}
	g.AllowLive = true
	if _, e = g.SubmitPreauthorization(ctx, second.TradeNo, 7, "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	g.AllowLive = false
	mode = "auth-unknown-payment-success"
	result, e = g.Query(ctx, 2, second.TradeNo)
	if e != nil || result.State != "TRADE_SUCCESS" {
		t.Fatal("independent verified payment blocked by authorization query", result, e)
	}
	balance, _ = s.Store.Balance(ctx, 2)
	if balance.Available != 125 {
		t.Fatal("verified payment not credited", balance)
	}
}
