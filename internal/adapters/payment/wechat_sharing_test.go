package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWechatChannelSharingTLSChecksEveryReceiverAndOriginalReturn(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	mode, calls := "partial", 0
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var input map[string]json.RawMessage
		if r.Method == "POST" {
			json.NewDecoder(r.Body).Decode(&input)
		}
		var result any
		switch r.URL.Path {
		case "/v3/profitsharing/receivers/add":
			if r.Header.Get("Wechatpay-Serial") != "PUB_KEY_ID_LOCAL" {
				t.Error("encrypted recipient did not select its exact public key serial")
			}
			encrypted, _ := base64.StdEncoding.DecodeString(v3Text(input, "name",512))
			name, e := rsa.DecryptOAEP(sha1.New(), rand.Reader, platform, encrypted, nil)
			if e != nil || string(name) != "本地商户" {
				t.Error("recipient name is not OAEP encrypted", e)
			}
			result = map[string]any{"type": "MERCHANT_ID", "account": "receiver-one", "relation_type": "SUPPLIER"}
		case "/v3/profitsharing/orders", "/v3/profitsharing/orders/SHARE_1":
			if r.Method == "GET" && r.URL.Query().Get("transaction_id") != "WX_PAYMENT" {
				t.Error("query lost original payment")
			}
			receivers := []map[string]any{{"type": "MERCHANT_ID", "account": "receiver-one", "amount": 10, "result": "SUCCESS", "detail_id": "DETAIL_1"}, {"type": "MERCHANT_ID", "account": "receiver-two", "amount": 20, "result": "CLOSED", "fail_reason": "NO_AUTH", "detail_id": "DETAIL_2"}}
			if mode == "wrong-amount" {
				receivers[1]["amount"] = 21
			}
			if mode == "duplicate" {
				receivers[1] = receivers[0]
			}
			result = map[string]any{"transaction_id": "WX_PAYMENT", "out_order_no": "SHARE_1", "order_id": "WX_SHARE", "state": "FINISHED", "receivers": receivers}
		case "/v3/profitsharing/return-orders", "/v3/profitsharing/return-orders/RETURN_1":
			amount := int64(10)
			if mode == "wrong-return" {
				amount = 11
			}
			result = map[string]any{"order_id": "WX_SHARE", "out_order_no": "SHARE_1", "out_return_no": "RETURN_1", "return_id": "WX_RETURN", "return_mchid": "receiver-one", "amount": amount, "result": "SUCCESS"}
		default:
			t.Error("unexpected sharing endpoint", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		raw, _ := json.Marshal(result)
		v3SignedResponse(t, w, platform, 200, string(raw), "PUB_KEY_ID_LOCAL")
	}))
	defer up.Close()
	p, e := NewWechatV3(WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL": &platform.PublicKey}, EnableChannelSharing: true, HTTPClient: &http.Client{Transport: v3TLSTransport{up, t}}})
	if e != nil {
		t.Fatal(e)
	}
	receiver := SharingReceiver{Type: "MERCHANT_ID", Account: "receiver-one", Name: "本地商户", RelationType: "SUPPLIER"}
	if e = p.BindChannelSharingReceiver(context.Background(), "BIND_1", receiver); e != nil {
		t.Fatal(e)
	}
	request := ChannelSharingRequest{OrderNo: "SHARE_1", ProviderTradeNo: "WX_PAYMENT", TotalAmount: 101, Receivers: []SharingAllocation{{SharingReceiver: receiver, Amount: 10, Description: "本地分账"}, {SharingReceiver: SharingReceiver{Type: "MERCHANT_ID", Account: "receiver-two"}, Amount: 20, Description: "本地分账"}}}
	result, e := p.SubmitChannelSharing(context.Background(), request)
	if e != nil || result.State != "partial" || len(result.Receivers) != 2 || result.Receivers[1].State != "failed" {
		t.Fatal("first receiver success is not whole-order success", result, e)
	}
	for _, bad := range []string{"wrong-amount", "duplicate"} {
		mode = bad
		if _, e = p.QueryChannelSharing(context.Background(), request); e == nil {
			t.Fatal("invalid receiver evidence accepted", bad)
		}
	}
	mode = "partial"
	returned := ChannelSharingReturnRequest{OrderNo: "SHARE_1", ProviderOrderNo: "WX_SHARE", ReturnNo: "RETURN_1", ReceiverType: "MERCHANT_ID", ReceiverAccount: "receiver-one", Amount: 10, Description: "本地退分账"}
	if result, e := p.ReturnChannelSharing(context.Background(), returned); e != nil || result.State != "succeeded" {
		t.Fatal(result, e)
	}
	mode = "wrong-return"
	if _, e = p.QueryChannelSharingReturn(context.Background(), returned); e == nil {
		t.Fatal("different returned amount accepted")
	}
	var unknown *UnknownOutcome
	if _, e = p.ReturnChannelSharing(context.Background(), returned); !errors.As(e, &unknown) {
		t.Fatal("invalid write reply must stay unknown", e)
	}
	before := calls
	request.Receivers[1].Account = "receiver-one"
	if _, e = p.SubmitChannelSharing(context.Background(), request); e == nil || calls != before {
		t.Fatal("invalid duplicated request reached upstream")
	}
}
