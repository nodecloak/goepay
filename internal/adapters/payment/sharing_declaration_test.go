package payment

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestChannelSharingDeclarationRequiresExplicitSupportedPaymentProduct(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	calls := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&request)
		var settle struct {
			ProfitSharing bool `json:"profit_sharing"`
		}
		if json.Unmarshal(request["settle_info"], &settle) != nil || !settle.ProfitSharing {
			t.Error("original payment did not declare sharing")
		}
		v3SignedResponse(t, w, platform, 200, `{"code_url":"weixin://wxpay/bizpayurl?pr=local"}`, "PUB_KEY_ID_LOCAL")
	}))
	defer upstream.Close()
	wx, e := NewWechatV3(WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL": &platform.PublicKey}, EnableChannelSharing: true, HTTPClient: &http.Client{Transport: v3TLSTransport{upstream, t}}})
	if e != nil {
		t.Fatal(e)
	}
	request := CreateRequest{TradeNo: "ORDER_1", Method: "native", Subject: "local", Amount: 101, NotifyURL: "https://pay.example.invalid/callback", ProfitSharing: true}
	if _, e = wx.Create(context.Background(), request); e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
	wx.config.EnableChannelSharing = false
	if _, e = wx.Create(context.Background(), request); e == nil || calls != 1 {
		t.Fatal("sharing declaration bypassed enablement")
	}
	old, _ := NewWechatV2(WechatV2Config{AppID: "local-app", MerchantID: "local-mch", APIKey: "0123456789abcdef0123456789abcdef", BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	if _, e = old.Create(context.Background(), request); e == nil || calls != 1 {
		t.Fatal("V2 silently ignored a V3 sharing declaration")
	}
	ali, e := NewAlipay(AlipayConfig{AppID: "local-app", SellerID: "2088000000000099", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: upstream.URL, HTTPClient: upstream.Client(), Methods: []string{"app", "native"}, EnableChannelSharing: true})
	if e != nil {
		t.Fatal(e)
	}
	request.Method = "app"
	result, e := ali.Create(context.Background(), request)
	if e != nil {
		t.Fatal(e)
	}
	values, e := url.ParseQuery(result.Material)
	if e != nil {
		t.Fatal(e)
	}
	var biz struct {
		Extend struct {
			Freeze string `json:"royalty_freeze"`
		} `json:"extend_params"`
	}
	if json.Unmarshal([]byte(values.Get("biz_content")), &biz) != nil || biz.Extend.Freeze != "true" {
		t.Fatal("Alipay SDK material omitted immutable royalty freeze")
	}
	request.Method = "native"
	if _, e = ali.Create(context.Background(), request); e == nil || calls != 1 {
		t.Fatal("face-to-face automatic refund semantics were silently treated as merchant-funded sharing")
	}
}
