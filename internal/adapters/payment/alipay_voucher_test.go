package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAlipayPreauthVoucherCreatesAuthenticatedQRWithoutPaymentEvidence(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	calls := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if e := r.ParseForm(); e != nil {
			t.Error(e)
		}
		p := map[string]string{}
		for k, v := range r.PostForm {
			p[k] = v[0]
		}
		if p["method"] != "alipay.fund.auth.order.voucher.create" || !verifyRSA([]byte(AlipayRequestCanonical(p)), p["sign"], &merchant.PublicKey) || p["notify_url"] != "https://pay.example.invalid/pay/preauthnotify/voucher1/" {
			t.Error("wrong voucher request or signature")
		}
		var biz map[string]any
		json.Unmarshal([]byte(p["biz_content"]), &biz)
		if biz["out_order_no"] != "voucher1" || biz["out_request_no"] != "voucher1" || biz["product_code"] != "PRE_AUTH" || biz["amount"] != "1.01" || biz["pay_timeout"] != "2h" {
			t.Error("wrong voucher identity, product or amount", biz)
		}
		raw := `{"code":"10000","out_order_no":"voucher1","out_request_no":"voucher1","code_type":"qrCode","code_value":"https://qr.alipay.com/local-auth-voucher"}`
		json.NewEncoder(w).Encode(map[string]any{"alipay_fund_auth_order_voucher_create_response": json.RawMessage(raw), "sign": independentSignature(t, raw, platform)})
	}))
	defer upstream.Close()
	p, e := NewAlipay(AlipayConfig{AppID: "app", SellerID: "seller", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, PreauthVoucher: true, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	got, e := p.Freeze(context.Background(), CreateRequest{TradeNo: "voucher1", Subject: "authorization", Amount: 101, Method: "preauth", NotifyURL: "https://pay.example.invalid/pay/preauthnotify/voucher1/"})
	if e != nil || got.MaterialType != "qrcode" || got.Material != "https://qr.alipay.com/local-auth-voucher" || got.ProviderTradeNo != "" || calls != 1 {
		t.Fatalf("voucher is not a paid trade: %+v calls=%d err=%v", got, calls, e)
	}
}

func TestAlipayPreauthVoucherRejectsMismatchedOrUnsafeMaterial(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	for _, tc := range []struct{ key, value string }{
		{"out_order_no", "other"}, {"out_request_no", "other"}, {"code_type", "barCode"}, {"code_value", "http://qr.alipay.com/x"}, {"code_value", "https://qr.alipay.com.evil.invalid/x"}, {"code_value", "https://name@qr.alipay.com/x"}, {"code_value", "https://qr.alipay.com:444/x"}, {"code_value", "https://qr.alipay.com/x#secret"}, {"code_value", ""},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				out := map[string]string{"code": "10000", "out_order_no": "voucher1", "out_request_no": "voucher1", "code_type": "qrCode", "code_value": "https://qr.alipay.com/test"}
				out[tc.key] = tc.value
				raw, _ := json.Marshal(out)
				json.NewEncoder(w).Encode(map[string]any{"alipay_fund_auth_order_voucher_create_response": json.RawMessage(raw), "sign": independentSignature(t, string(raw), platform)})
			}))
			defer upstream.Close()
			p, e := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, PreauthVoucher: true, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
			if e != nil {
				t.Fatal(e)
			}
			result, e := p.Freeze(context.Background(), CreateRequest{TradeNo: "voucher1", Subject: "authorization", Amount: 101, Method: "preauth", NotifyURL: "https://pay.example.invalid/preauth/"})
			if e == nil || result.Material != "" {
				t.Fatalf("unsafe voucher accepted key=%s", tc.key)
			}
		})
	}
}

func TestAlipayPreauthVoucherExplicitLegacyProductProfile(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var biz map[string]any
		json.Unmarshal([]byte(r.PostForm.Get("biz_content")), &biz)
		if biz["product_code"] != "PREAUTH_PAY" {
			t.Error("explicit Pro product profile was replaced")
		}
		raw := `{"code":"10000","out_order_no":"voucher1","out_request_no":"voucher1","code_type":"qrCode","code_value":"https://qr.alipay.com/local"}`
		json.NewEncoder(w).Encode(map[string]any{"alipay_fund_auth_order_voucher_create_response": json.RawMessage(raw), "sign": independentSignature(t, raw, platform)})
	}))
	defer upstream.Close()
	cfg := AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, PreauthVoucher: true, PreauthVoucherProductCode: "PREAUTH_PAY", GatewayURL: upstream.URL, HTTPClient: upstream.Client()}
	p, e := NewAlipay(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Freeze(context.Background(), CreateRequest{TradeNo: "voucher1", Subject: "auth", Amount: 1, Method: "preauth", NotifyURL: "https://pay.example.invalid/preauth/"}); e != nil {
		t.Fatal(e)
	}
	cfg.PreauthVoucherProductCode = "unknown"
	if _, e = NewAlipay(cfg); e == nil {
		t.Fatal("arbitrary product accepted")
	}
	cfg.PreauthVoucherProductCode = "PREAUTH_PAY"
	cfg.PreauthVoucher = false
	if _, e = NewAlipay(cfg); e == nil {
		t.Fatal("voucher product applied to SDK freeze")
	}
}
