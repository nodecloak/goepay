package payment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestAlipayOrderCodeTLSLifecycle(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	now := time.Unix(1800000000, 0)
	expiry := now.Add(time.Hour)
	for _, offline := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "order-code"}[offline], func(t *testing.T) {
			calls := 0
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.TLS == nil || r.Method != http.MethodPost {
					t.Error("expected local TLS POST")
				}
				r.ParseForm()
				p := map[string]string{}
				for key, values := range r.Form {
					if len(values) != 1 {
						t.Error("duplicate request field")
					}
					p[key] = values[0]
				}
				if p["app_id"] != "synthetic-app" || !verifyRSA([]byte(AlipayRequestCanonical(p)), p["sign"], &merchant.PublicKey) {
					t.Error("request signature/identity")
				}
				var biz map[string]any
				if json.Unmarshal([]byte(p["biz_content"]), &biz) != nil || biz["out_trade_no"] != "ORDER8" {
					t.Error("original order missing")
				}
				node := map[string]any{"code": "10000", "out_trade_no": "ORDER8", "trade_no": "ALI8"}
				switch p["method"] {
				case "alipay.trade.precreate":
					want := any(nil)
					if offline {
						want = "QR_CODE_OFFLINE"
					}
					if biz["product_code"] != want || biz["total_amount"] != "1.01" || biz["subject"] != "中文订单码 + &" || biz["seller_id"] != "2088000000000000" || biz["time_expire"] != expiry.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05") || p["notify_url"] != "https://pay.example.invalid/notify" {
						t.Errorf("precreate product/amount/original binding: %+v", biz)
					}
					bp, _ := biz["business_params"].(map[string]any)
					if len(bp) != 1 || bp["mc_create_trade_ip"] != "192.0.2.1" {
						t.Errorf("trusted IP not preserved: %+v", biz)
					}
					node["qr_code"] = "https://qr.alipay.com/synthetic-order8"
				case "alipay.trade.query":
					node["total_amount"] = "1.01"
					node["trade_status"] = "TRADE_SUCCESS"
					node["buyer_user_id"] = "2088000000000001"
				case "alipay.trade.refund":
					if biz["out_request_no"] != "REFUND8" || biz["refund_amount"] != "1.01" {
						t.Error("refund original binding", biz)
					}
					node["refund_fee"] = "1.01"
				case "alipay.trade.fastpay.refund.query":
					node["out_request_no"] = "REFUND8"
					node["refund_amount"] = "1.01"
					node["refund_status"] = "REFUND_SUCCESS"
				default:
					t.Error("unexpected API", p["method"])
				}
				raw, _ := json.Marshal(node)
				json.NewEncoder(w).Encode(map[string]any{responseNode(p["method"]): json.RawMessage(raw), "sign": independentSignature(t, string(raw), platform)})
			}))
			defer upstream.Close()
			c := AlipayConfig{AppID: "synthetic-app", SellerID: "2088000000000000", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"native"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client(), Now: func() time.Time { return now }}
			c.NativeOrderCode = offline
			p, err := NewAlipay(c)
			if err != nil {
				t.Fatal(err)
			}
			result, err := p.Create(context.Background(), CreateRequest{TradeNo: "ORDER8", Subject: "中文订单码 + &", Method: "native", Amount: 101, ClientIP: "192.0.2.1", NotifyURL: "https://pay.example.invalid/notify", ExpiresAt: &expiry})
			if err != nil || result.MaterialType != "qrcode" || result.Material != "https://qr.alipay.com/synthetic-order8" {
				t.Fatal(result, err)
			}
			query, err := p.Query(context.Background(), QueryRequest{TradeNo: "ORDER8"})
			if err != nil || query.Amount != 101 || query.State != "TRADE_SUCCESS" {
				t.Fatal(query, err)
			}
			r := RefundRequest{TradeNo: "ORDER8", RefundNo: "REFUND8", Amount: 101, TotalAmount: 101}
			refund, err := p.Refund(context.Background(), r)
			if err != nil || refund.Amount != 101 || refund.State != "SUCCESS" {
				t.Fatal(refund, err)
			}
			if _, err = p.QueryRefund(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			callback := map[string]string{"app_id": "synthetic-app", "seller_id": "2088000000000000", "out_trade_no": "ORDER8", "trade_no": "ALI8", "trade_status": "TRADE_SUCCESS", "total_amount": "1.01", "sign_type": "RSA2"}
			callback["sign"] = independentSignature(t, AlipayNotificationCanonical(callback), platform)
			form := url.Values{}
			for k, v := range callback {
				form.Set(k, v)
			}
			if _, err = p.VerifyNotification(context.Background(), nil, []byte(form.Encode()), ExpectedPayment{TradeNo: "ORDER8", Amount: 101, AppID: "synthetic-app", MerchantID: "2088000000000000"}); err != nil {
				t.Fatal(err)
			}
			before := calls
			if _, err = p.Create(context.Background(), CreateRequest{TradeNo: "ORDER8", Subject: "test", Method: "native", Amount: 101, ClientIP: "bad\nIP"}); !errors.Is(err, ErrInvalid) {
				t.Error("invalid IP accepted", err)
			}
			if calls != before {
				t.Error("invalid IP reached provider")
			}
		})
	}
}

func TestAlipayOrderCodeMissingIPAndUncertainResponses(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	for _, kind := range []string{"ok", "wrong-order", "bad-signature", "duplicate", "http-failure"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				r.ParseForm()
				var biz map[string]any
				json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
				if biz["product_code"] != "QR_CODE_OFFLINE" || biz["business_params"] != nil {
					t.Error("missing IP was synthesized or order-code lost", biz)
				}
				if kind == "http-failure" {
					w.WriteHeader(503)
					return
				}
				node := `{"code":"10000","out_trade_no":"ORDER8","qr_code":"https://qr.alipay.com/synthetic"}`
				if kind == "wrong-order" {
					node = `{"code":"10000","out_trade_no":"OTHER","qr_code":"https://qr.alipay.com/synthetic"}`
				}
				if kind == "duplicate" {
					node = `{"code":"10000","out_trade_no":"OTHER","out_trade_no":"ORDER8","qr_code":"https://qr.alipay.com/synthetic"}`
				}
				sign := independentSignature(t, node, platform)
				if kind == "bad-signature" {
					sign = independentSignature(t, node, merchant)
				}
				json.NewEncoder(w).Encode(map[string]any{"alipay_trade_precreate_response": json.RawMessage(node), "sign": sign})
			}))
			defer upstream.Close()
			p, err := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"native"}, NativeOrderCode: true, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
			if err != nil {
				t.Fatal(err)
			}
			result, err := p.Create(context.Background(), CreateRequest{TradeNo: "ORDER8", Subject: "test", Method: "native", Amount: 101})
			if kind == "ok" {
				if err != nil || result.MaterialType != "qrcode" {
					t.Fatal(result, err)
				}
			} else {
				var unknown *UnknownOutcome
				if !errors.As(err, &unknown) || result.Material != "" {
					t.Fatal("uncertain write became usable material/known failure", result, err)
				}
			}
			if calls != 1 {
				t.Fatal("unexpected automatic retry", calls)
			}
		})
	}
	if _, err := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"page"}, NativeOrderCode: true}); !errors.Is(err, ErrInvalid) {
		t.Fatal("order-code flag without native accepted", err)
	}
}

func responseNode(method string) string {
	var out []byte
	for _, c := range []byte(method) {
		if c == '.' {
			c = '_'
		}
		out = append(out, c)
	}
	return string(out) + "_response"
}
