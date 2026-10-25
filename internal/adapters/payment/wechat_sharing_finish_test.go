package payment

import (
	"context"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWechatChannelSharingFinishRequiresSeparateOriginalResultProof(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	bad := false
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			v3SignedResponse(t, w, platform, 200, `{"out_order_no":"FINISH_1","transaction_id":"WX_PAYMENT","order_id":"WX_FINISH","state":"PROCESSING"}`, "PUB_KEY_ID_LOCAL")
			return
		}
		if r.URL.Path != "/v3/profitsharing/orders/FINISH_1" || r.URL.Query().Get("transaction_id") != "WX_PAYMENT" {
			t.Error("finish query did not use original intent")
		}
		amount := "71"
		if bad {
			amount = "72"
		}
		v3SignedResponse(t, w, platform, 200, `{"out_order_no":"FINISH_1","transaction_id":"WX_PAYMENT","order_id":"WX_FINISH","state":"FINISHED","receivers":[{"type":"MERCHANT_ID","account":"local-mch","amount":`+amount+`,"result":"SUCCESS","detail_id":"FINISH_DETAIL"}]}`, "PUB_KEY_ID_LOCAL")
	}))
	defer up.Close()
	p, e := NewWechatV3(WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL": &platform.PublicKey}, EnableChannelSharing: true, HTTPClient: &http.Client{Transport: v3TLSTransport{up, t}}})
	if e != nil {
		t.Fatal(e)
	}
	request := ChannelSharingFinishRequest{OrderNo: "FINISH_1", ProviderTradeNo: "WX_PAYMENT", ExpectedUnsplit: 71, Description: "解冻剩余资金"}
	result, e := p.FinishChannelSharing(context.Background(), request)
	if e != nil || result.State != "pending" {
		t.Fatal("accepted finish must remain pending", result, e)
	}
	request.ProviderOrderNo = result.ProviderOrderNo
	result, e = p.QueryChannelSharingFinish(context.Background(), request)
	if e != nil || result.State != "succeeded" || result.UnsplitAmount != 71 {
		t.Fatal(result, e)
	}
	bad = true
	if _, e = p.QueryChannelSharingFinish(context.Background(), request); e == nil {
		t.Fatal("wrong unfreeze amount accepted")
	}
}

func TestWechatChannelSharingRemainingRequiresExplicitIntegerAndOriginalTrade(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	raw := `{"transaction_id":"WX_PAYMENT","unsplit_amount":0}`
	calls := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v3/profitsharing/transactions/WX_PAYMENT/amounts" {
			t.Error("unexpected original transaction path", r.URL.Path)
		}
		v3SignedResponse(t, w, platform, 200, raw, "PUB_KEY_ID_LOCAL")
	}))
	defer upstream.Close()
	p, e := NewWechatV3(WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL": &platform.PublicKey}, EnableChannelSharing: true, HTTPClient: &http.Client{Transport: v3TLSTransport{upstream, t}}})
	if e != nil {
		t.Fatal(e)
	}
	if amount, e := p.ChannelSharingRemaining(context.Background(), "WX_PAYMENT"); e != nil || amount != 0 {
		t.Fatal(amount, e)
	}
	for _, bad := range []string{`{"transaction_id":"WX_PAYMENT"}`, `{"transaction_id":"FOREIGN","unsplit_amount":0}`, `{"transaction_id":"WX_PAYMENT","unsplit_amount":null}`, `{"transaction_id":"WX_PAYMENT","unsplit_amount":-1}`, `{"transaction_id":"WX_PAYMENT","unsplit_amount":0.5}`} {
		raw = bad
		if _, e = p.ChannelSharingRemaining(context.Background(), "WX_PAYMENT"); e == nil {
			t.Fatal("missing/malformed remaining value became a trusted zero", bad)
		}
	}
	before := calls
	if _, e = p.FinishChannelSharing(context.Background(), ChannelSharingFinishRequest{OrderNo: "FINISH_1", ProviderTradeNo: "WX_PAYMENT", Description: "零剩余", ExpectedUnsplit: 0}); e == nil || calls != before {
		t.Fatal("known zero remaining must not send an unqueryable unfreeze")
	}
	if _, e = p.ChannelSharingRemaining(context.Background(), "../foreign"); e == nil || calls != before {
		t.Fatal("provider transaction escaped its fixed route")
	}
}
