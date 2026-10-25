package payment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAlipayJSAPIAndMiniTrustedBuyerTradeCreate(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	calls := 0
	badResponse := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		r.ParseForm()
		params := map[string]string{}
		for k, v := range r.Form {
			params[k] = v[0]
		}
		if params["method"] != "alipay.trade.create" || params["notify_url"] != "https://pay.example.invalid/notify" || !verifyRSA([]byte(AlipayRequestCanonical(params)), params["sign"], &merchant.PublicKey) {
			t.Error("invalid signed create request")
		}
		var biz map[string]any
		json.Unmarshal([]byte(params["biz_content"]), &biz)
		switch biz["out_trade_no"] {
		case "jsapi-uid":
			if biz["buyer_id"] != "2088000000000000" || biz["buyer_open_id"] != nil || biz["product_code"] != nil {
				t.Error(biz)
			}
		case "jsapi-openid":
			if biz["buyer_open_id"] != "opaque-openid" || biz["op_buyer_open_id"] != nil {
				t.Error(biz)
			}
		case "mini":
			if biz["op_buyer_open_id"] != "opaque-openid" || biz["buyer_open_id"] != nil || biz["op_app_id"] != "app" || biz["product_code"] != "JSAPI_PAY" {
				t.Error(biz)
			}
		default:
			t.Error(biz)
		}
		node := map[string]any{"code": "10000", "trade_no": "synthetic-ali-42", "out_trade_no": biz["out_trade_no"]}
		if badResponse {
			node["out_trade_no"] = "different-order"
		}
		raw, _ := json.Marshal(node)
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_create_response": json.RawMessage(raw), "sign": independentSignature(t, string(raw), platform)})
	}))
	defer upstream.Close()
	p, e := NewAlipay(AlipayConfig{AppID: "app", SellerID: "seller", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"jsapi", "mini"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct{ method, trade, buyer string }{{"jsapi", "jsapi-uid", "2088000000000000"}, {"jsapi", "jsapi-openid", "opaque-openid"}, {"mini", "mini", "opaque-openid"}} {
		r := CreateRequest{TradeNo: tt.trade, Subject: "test", Method: tt.method, OpenID: tt.buyer, Amount: 100, NotifyURL: "https://pay.example.invalid/notify", ClientIP: "192.0.2.1"}
		result, e := p.Create(context.Background(), r)
		if e != nil || result.MaterialType != "jsapi" || result.Material != "synthetic-ali-42" || result.ProviderTradeNo != "synthetic-ali-42" {
			t.Fatal(tt, result, e)
		}
		if strings.HasPrefix(result.Material, "{") {
			t.Fatal("Alipay trade number was wrapped as Wechat JSON")
		}
	}
	before := calls
	for _, buyer := range []string{"", " ", "@untrusted", "bad\nvalue"} {
		if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "mini", Subject: "test", Method: "mini", OpenID: buyer, Amount: 100, NotifyURL: "https://pay.example.invalid/notify"}); !errors.Is(e, ErrInvalid) {
			t.Fatal("untrusted buyer accepted", buyer, e)
		}
	}
	if calls != before {
		t.Fatal("invalid buyer reached upstream")
	}
	badResponse = true
	_, e = p.Create(context.Background(), CreateRequest{TradeNo: "mini", Subject: "test", Method: "mini", OpenID: "opaque-openid", Amount: 100, NotifyURL: "https://pay.example.invalid/notify"})
	var unknown *UnknownOutcome
	if !errors.As(e, &unknown) {
		t.Fatal("wrong order response did not preserve unknown", e)
	}
}
