package payment

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func combineV3Provider(t *testing.T, handler func(path string, body map[string]json.RawMessage) (int, string)) (*WechatV3, *atomic.Int64) {
	t.Helper()
	merchant, platform := testKey(t), testKey(t)
	var calls atomic.Int64
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		if len(raw) > 0 && json.Unmarshal(raw, &body) != nil {
			t.Error("bad request JSON")
		}
		status, out := handler(r.URL.Path, body)
		v3SignedResponse(t, w, platform, status, out, "PUB_KEY_ID_LOCAL")
	}))
	t.Cleanup(up.Close)
	p, e := NewWechatV3(WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL": &platform.PublicKey}, Methods: []string{"native", "jsapi", "h5", "app"}, H5SiteURL: "https://shop.example.invalid", H5SiteName: "Local", HTTPClient: &http.Client{Transport: v3TLSTransport{up, t}}})
	if e != nil {
		t.Fatal(e)
	}
	return p, &calls
}

var combineV3Subs = []CombineSub{{"ORDER_11", 34}, {"ORDER_12", 34}, {"ORDER_13", 33}}

func combineV3Request(method string) CreateRequest {
	return CreateRequest{TradeNo: "ORDER_1", Subject: "合单测试", Method: method, NotifyURL: "https://pay.example.invalid/callbacks/providers/7/ORDER_1", ClientIP: "127.0.0.1", OpenID: "trusted-buyer", Amount: 101}
}

func TestWechatV3CombineCreateSendsOriginalCombinedOrderForEveryMethod(t *testing.T) {
	for _, method := range []string{"native", "jsapi", "h5", "app"} {
		t.Run(method, func(t *testing.T) {
			p, _ := combineV3Provider(t, func(path string, b map[string]json.RawMessage) (int, string) {
				if path != "/v3/combine-transactions/"+method {
					t.Error("wrong combined endpoint", path)
				}
				var app, mch, no string
				json.Unmarshal(b["combine_appid"], &app)
				json.Unmarshal(b["combine_mchid"], &mch)
				json.Unmarshal(b["combine_out_trade_no"], &no)
				if app != "local-app" || mch != "local-mch" || no != "ORDER_1" {
					t.Error("wrong combined identity", app, mch, no)
				}
				var subs []struct {
					OutTradeNo string `json:"out_trade_no"`
					Amount     struct {
						TotalAmount int64  `json:"total_amount"`
						Currency    string `json:"currency"`
					} `json:"amount"`
				}
				var rawSubs []map[string]json.RawMessage
				json.Unmarshal(b["sub_orders"], &rawSubs)
				json.Unmarshal(b["sub_orders"], &subs)
				if len(subs) != 3 {
					t.Fatal("sub_orders missing", string(b["sub_orders"]))
				}
				for i, s := range subs {
					var mchid, attach string
					json.Unmarshal(rawSubs[i]["mchid"], &mchid)
					json.Unmarshal(rawSubs[i]["attach"], &attach)
					if mchid != "local-mch" || attach != "combine" || s.OutTradeNo != combineV3Subs[i].TradeNo || s.Amount.TotalAmount != combineV3Subs[i].Amount || s.Amount.Currency != "CNY" {
						t.Error("wrong sub-order", i, string(b["sub_orders"]))
					}
				}
				_, payer := b["combine_payer_info"]
				if payer != (method == "jsapi") {
					t.Error("combine_payer_info presence wrong for", method)
				}
				if method == "h5" && !strings.Contains(string(b["scene_info"]), `"device_id":"10001"`) {
					t.Error("h5 scene_info device_id missing", string(b["scene_info"]))
				}
				switch method {
				case "native":
					return 200, `{"code_url":"weixin://wxpay/bizpayurl?pr=combine"}`
				case "h5":
					return 200, `{"h5_url":"https://wx.tenpay.com/cgi-bin/mmpayweb-bin/checkmweb?prepay_id=combine"}`
				}
				return 200, `{"prepay_id":"prepay-combine"}`
			})
			got, err := p.CombineCreate(context.Background(), combineV3Request(method), combineV3Subs)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"native": "qrcode", "h5": "jump", "jsapi": "jsapi", "app": "app"}[method]
			if got.MaterialType != want || got.Material == "" {
				t.Fatal("wrong combined material", got)
			}
		})
	}
}

func TestWechatV3CombineCreateRejectsBadSplitsWithoutSending(t *testing.T) {
	p, calls := combineV3Provider(t, func(string, map[string]json.RawMessage) (int, string) { return 500, "" })
	for name, subs := range map[string][]CombineSub{
		"two":        {{"ORDER_11", 51}, {"ORDER_12", 50}},
		"unbalanced": {{"ORDER_11", 34}, {"ORDER_12", 34}, {"ORDER_13", 34}},
		"duplicate":  {{"ORDER_11", 34}, {"ORDER_11", 34}, {"ORDER_13", 33}},
		"parent-no":  {{"ORDER_1", 34}, {"ORDER_12", 34}, {"ORDER_13", 33}},
		"zero":       {{"ORDER_11", 101}, {"ORDER_12", 0}, {"ORDER_13", 0}},
	} {
		if _, err := p.CombineCreate(context.Background(), combineV3Request("native"), subs); !errors.Is(err, ErrInvalid) {
			t.Error(name, "accepted", err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid combined order reached the provider")
	}
}

func TestWechatV3CombineCreateMalformedResponseIsUnknownOutcome(t *testing.T) {
	p, _ := combineV3Provider(t, func(string, map[string]json.RawMessage) (int, string) {
		return 200, `{"code_url":"https://evil.example/pay"}`
	})
	_, err := p.CombineCreate(context.Background(), combineV3Request("native"), combineV3Subs)
	var unknown *UnknownOutcome
	if !errors.As(err, &unknown) {
		t.Fatal("unexpected combined response must be an unknown outcome (query, never resend)", err)
	}
}

func combineV3Notify(t *testing.T, p *WechatV3, platform *rsa.PrivateKey, key []byte, plain string) (http.Header, []byte) {
	t.Helper()
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	n := []byte("0123456789ab")
	encrypted := gcm.Seal(nil, n, []byte(plain), []byte("transaction"))
	body, _ := json.Marshal(map[string]any{"id": "evt-combine", "create_time": time.Now().Format(time.RFC3339), "event_type": "TRANSACTION.SUCCESS", "resource_type": "encrypt-resource", "resource": map[string]string{"algorithm": "AEAD_AES_256_GCM", "original_type": "transaction", "nonce": string(n), "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(encrypted)}})
	recorder := httptest.NewRecorder()
	v3SignedResponse(t, recorder, platform, 200, string(body), "PUB_KEY_ID_LOCAL")
	return recorder.Header(), body
}

func combineV3Plain(mutate func(subs []map[string]any, top map[string]any)) string {
	subs := []map[string]any{}
	for i, s := range combineV3Subs {
		subs = append(subs, map[string]any{"mchid": "local-mch", "trade_type": "NATIVE", "trade_state": "SUCCESS", "transaction_id": "WX_SUB_" + strconv.Itoa(i+1), "out_trade_no": s.TradeNo, "amount": map[string]any{"total_amount": s.Amount, "currency": "CNY"}})
	}
	top := map[string]any{"combine_appid": "local-app", "combine_mchid": "local-mch", "combine_out_trade_no": "ORDER_1", "sub_orders": subs, "combine_payer_info": map[string]string{"openid": "trusted-buyer"}}
	if mutate != nil {
		mutate(subs, top)
	}
	raw, _ := json.Marshal(top)
	return string(raw)
}

func TestWechatV3CombinedNotificationVerifiesEverySubOrder(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	p, e := NewWechatV3(WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchant, APIv3Key: key, VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL": &platform.PublicKey}})
	if e != nil {
		t.Fatal(e)
	}
	expected := ExpectedPayment{TradeNo: "ORDER_1", Amount: 101, CombineSubs: combineV3Subs}
	h, body := combineV3Notify(t, p, platform, key, combineV3Plain(nil))
	event, err := p.VerifyNotification(context.Background(), h, body, expected)
	if err != nil || event.State != "SUCCESS" || event.Amount != 101 || event.EventID != "evt-combine" || len(event.CombinePaid) != 3 || event.CombinePaid[0].TransactionID != "WX_SUB_1" || event.BuyerID != "trusted-buyer" {
		t.Fatal("valid combined notification rejected", event, err)
	}
	for name, mutate := range map[string]func([]map[string]any, map[string]any){
		"foreign-combine-mch": func(_ []map[string]any, top map[string]any) { top["combine_mchid"] = "foreign" },
		"foreign-sub-mch":     func(s []map[string]any, _ map[string]any) { s[1]["mchid"] = "foreign" },
		"wrong-parent":        func(_ []map[string]any, top map[string]any) { top["combine_out_trade_no"] = "ORDER_2" },
		"wrong-sub-amount": func(s []map[string]any, _ map[string]any) {
			s[2]["amount"] = map[string]any{"total_amount": 34, "currency": "CNY"}
		},
		"missing-sub": func(_ []map[string]any, top map[string]any) {
			top["sub_orders"] = top["sub_orders"].([]map[string]any)[:2]
		},
		"partial-paid": func(s []map[string]any, _ map[string]any) {
			s[0]["trade_state"] = "NOTPAY"
			delete(s[0], "transaction_id")
		},
		"no-txid":       func(s []map[string]any, _ map[string]any) { delete(s[1], "transaction_id") },
		"unknown-sub":   func(s []map[string]any, _ map[string]any) { s[0]["out_trade_no"] = "ORDER_19" },
		"no-payer":      func(_ []map[string]any, top map[string]any) { delete(top, "combine_payer_info") },
		"duplicate-sub": func(s []map[string]any, _ map[string]any) { s[1]["out_trade_no"] = "ORDER_11" },
	} {
		h, body := combineV3Notify(t, p, platform, key, combineV3Plain(mutate))
		if _, err := p.VerifyNotification(context.Background(), h, body, expected); err == nil {
			t.Error(name, "accepted")
		}
	}
	// A combined payload never authorizes an ordinary order, and vice versa.
	if _, err := p.VerifyNotification(context.Background(), h, body, ExpectedPayment{TradeNo: "ORDER_1", Amount: 101}); err == nil {
		t.Error("combined notification accepted for an ordinary order")
	}
}

func TestWechatV3CombineQueryAndCloseUseCombinedEndpoints(t *testing.T) {
	var closeBody map[string]json.RawMessage
	state := "SUCCESS"
	p, _ := combineV3Provider(t, func(path string, b map[string]json.RawMessage) (int, string) {
		switch path {
		case "/v3/combine-transactions/out-trade-no/ORDER_1":
			return 200, combineV3Plain(func(s []map[string]any, _ map[string]any) {
				if state != "SUCCESS" {
					for _, sub := range s {
						sub["trade_state"] = state
						delete(sub, "transaction_id")
					}
				}
			})
		case "/v3/combine-transactions/out-trade-no/ORDER_1/close":
			closeBody = b
			return 204, ""
		}
		t.Error("unexpected endpoint", path)
		return 500, ""
	})
	q := QueryRequest{TradeNo: "ORDER_1", CombineSubs: combineV3Subs}
	got, err := p.Query(context.Background(), q)
	if err != nil || got.State != "SUCCESS" || got.Amount != 101 || len(got.CombinePaid) != 3 {
		t.Fatal("combined query", got, err)
	}
	state = "NOTPAY"
	if got, err = p.Query(context.Background(), q); err != nil || got.State != "NOTPAY" || len(got.CombinePaid) != 0 {
		t.Fatal("unpaid combined query", got, err)
	}
	if err = p.Close(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	var app string
	json.Unmarshal(closeBody["combine_appid"], &app)
	var subs []map[string]string
	json.Unmarshal(closeBody["sub_orders"], &subs)
	if app != "local-app" || len(subs) != 3 || subs[0]["mchid"] != "local-mch" || subs[2]["out_trade_no"] != "ORDER_13" {
		t.Fatal("combined close body wrong", closeBody)
	}
}
