package payment

import (
	"context"
	"net/url"
	"testing"
)

func TestAlipayVerifiedBuyerOpenIDFallbackAndExcludedMarkerRejected(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	a, e := NewAlipay(AlipayConfig{AppID: "app", SellerID: "seller", PrivateKey: merchant, PublicKey: &platform.PublicKey})
	if e != nil {
		t.Fatal(e)
	}
	params := map[string]string{"app_id": "app", "seller_id": "seller", "trade_no": "ali42", "out_trade_no": "42", "trade_status": "TRADE_SUCCESS", "total_amount": "1.00", "sign_type": "RSA2", "buyer_open_id": "synthetic-openid"}
	invoke := func() (PaymentEvent, error) {
		params["sign"] = independentSignature(t, AlipayNotificationCanonical(params), platform)
		values := url.Values{}
		for k, v := range params {
			values.Set(k, v)
		}
		return a.VerifyNotification(context.Background(), nil, []byte(values.Encode()), ExpectedPayment{TradeNo: "42", Amount: 100})
	}
	event, e := invoke()
	if e != nil || event.BuyerID != "synthetic-openid" {
		t.Fatal("verified OpenID lost", event, e)
	}
	params["buyer_id"] = "@unsigned-file-marker"
	if _, e = invoke(); e == nil {
		t.Fatal("SDK-excluded buyer field trusted")
	}
}
