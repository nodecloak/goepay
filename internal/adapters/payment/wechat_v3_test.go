package payment

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWechatV3DirectPaymentTLSProductsAndOriginalRefundEvidence(t *testing.T) {
	merchant := testKey(t)
	platform := testKey(t)
	badIdentity := false
	calls := 0
	transaction := `{"appid":"local-app","mchid":"local-mch","out_trade_no":"ORDER_1","transaction_id":"WX_ORIGINAL","trade_state":"SUCCESS","amount":{"total":101,"currency":"CNY"},"payer":{"openid":"trusted-buyer"}}`
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		var p map[string]json.RawMessage
		if len(body) > 0 && json.Unmarshal(body, &p) != nil {
			t.Error("bad request JSON")
		}
		path := r.URL.Path
		switch {
		case strings.HasPrefix(path, "/v3/pay/transactions/") && r.Method == "POST" && !strings.HasSuffix(path, "/close"):
			var app, mch, trade string
			json.Unmarshal(p["appid"], &app)
			json.Unmarshal(p["mchid"], &mch)
			json.Unmarshal(p["out_trade_no"], &trade)
			var amount struct {
				Total    int64
				Currency string
			}
			json.Unmarshal(p["amount"], &amount)
			if app != "local-app" || mch != "local-mch" || trade != "ORDER_1" || amount.Total != 101 || amount.Currency != "CNY" {
				t.Error("wrong original payment identity or amount")
			}
			if _, exists := p["settle_info"]; exists {
				t.Error("sharing must require a separate durable order declaration")
			}
			result := `{"prepay_id":"prepay-local"}`
			if strings.HasSuffix(path, "/native") {
				result = `{"code_url":"weixin://wxpay/bizpayurl?pr=synthetic"}`
			}
			if strings.HasSuffix(path, "/h5") {
				result = `{"h5_url":"https://wx.tenpay.com/cgi-bin/mmpayweb-bin/checkmweb?prepay_id=synthetic"}`
			}
			v3SignedResponse(t, w, platform, 200, result, "PUB_KEY_ID_LOCAL")
		case r.Method == "GET" && path == "/v3/pay/transactions/out-trade-no/ORDER_1":
			if r.URL.Query().Get("mchid") != "local-mch" {
				t.Error("query account missing")
			}
			raw := transaction
			if badIdentity {
				raw = strings.Replace(raw, "local-mch", "foreign-mch", 1)
			}
			v3SignedResponse(t, w, platform, 200, raw, "PUB_KEY_ID_LOCAL")
		case strings.HasSuffix(path, "/close"):
			v3SignedResponse(t, w, platform, 204, "", "PUB_KEY_ID_LOCAL")
		case path == "/v3/refund/domestic/refunds" || path == "/v3/refund/domestic/refunds/REFUND_1":
			status := "PROCESSING"
			if r.Method == "GET" {
				status = "SUCCESS"
			}
			raw := `{"out_trade_no":"ORDER_1","transaction_id":"WX_ORIGINAL","out_refund_no":"REFUND_1","refund_id":"WX_REFUND","status":"` + status + `","amount":{"total":101,"refund":40,"currency":"CNY"}}`
			if r.Method == "POST" {
				var a struct {
					Total, Refund int64
					Currency      string
				}
				json.Unmarshal(p["amount"], &a)
				if a.Total != 101 || a.Refund != 40 || a.Currency != "CNY" {
					t.Error("wrong refund amount")
				}
			}
			if badIdentity {
				raw = strings.Replace(raw, `"refund":40`, `"refund":41`, 1)
			}
			v3SignedResponse(t, w, platform, 200, raw, "PUB_KEY_ID_LOCAL")
		default:
			t.Error("unexpected API", r.Method, path)
			w.WriteHeader(500)
		}
	}))
	defer up.Close()
	p, e := NewWechatV3(WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL": &platform.PublicKey}, Methods: []string{"native", "jsapi", "h5", "app"}, H5SiteURL: "https://shop.example.invalid", H5SiteName: "Local", HTTPClient: &http.Client{Transport: v3TLSTransport{up, t}}})
	if e != nil {
		t.Fatal(e)
	}
	var _ Provider = p
	var _ NotificationVerifier = p
	var _ TransferAccountIdentity = p
	for _, method := range []string{"native", "jsapi", "h5", "app"} {
		t.Run(method, func(t *testing.T) {
			result, e := p.Create(context.Background(), CreateRequest{TradeNo: "ORDER_1", Subject: "本地支付测试", Method: method, NotifyURL: "https://pay.example.invalid/callbacks/providers/7/ORDER_1", ReturnURL: "https://pay.example.invalid/return", ClientIP: "127.0.0.1", OpenID: "trusted-buyer", Amount: 101})
			if e != nil {
				t.Fatal(e)
			}
			switch method {
			case "native":
				if result.MaterialType != "qrcode" || !strings.HasPrefix(result.Material, "weixin://wxpay/") {
					t.Fatal(result)
				}
			case "h5":
				if result.MaterialType != "jump" || !strings.Contains(result.Material, "redirect_url=") {
					t.Fatal(result)
				}
			default:
				var sdk map[string]string
				if json.Unmarshal([]byte(result.Material), &sdk) != nil {
					t.Fatal("bad SDK material")
				}
				stamp, n, prepay, signature := sdk["timeStamp"], sdk["nonceStr"], sdk["package"], sdk["paySign"]
				if method == "app" {
					stamp, n, prepay, signature = sdk["timestamp"], sdk["noncestr"], sdk["prepayid"], sdk["sign"]
					if sdk["partnerid"] != "local-mch" || sdk["package"] != "Sign=WXPay" {
						t.Fatal(sdk)
					}
				} else {
					if sdk["signType"] != "RSA" || sdk["appId"] != "local-app" {
						t.Fatal(sdk)
					}
				}
				sum := sha256.Sum256([]byte("local-app\n" + stamp + "\n" + n + "\n" + prepay + "\n"))
				sig, _ := base64.StdEncoding.DecodeString(signature)
				if rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, sum[:], sig) != nil {
					t.Fatal("incorrect RSA SDK signature")
				}
			}
		})
	}
	q, e := p.Query(context.Background(), QueryRequest{TradeNo: "ORDER_1", ProviderTradeNo: "WX_ORIGINAL"})
	if e != nil || q.Amount != 101 || q.BuyerID != "trusted-buyer" || q.State != "SUCCESS" {
		t.Fatal(q, e)
	}
	refund := RefundRequest{TradeNo: "ORDER_1", ProviderTradeNo: "WX_ORIGINAL", RefundNo: "REFUND_1", TotalAmount: 101, Amount: 40}
	if r, e := p.Refund(context.Background(), refund); e != nil || r.State != "PROCESSING" {
		t.Fatal(r, e)
	}
	if r, e := p.QueryRefund(context.Background(), refund); e != nil || r.State != "SUCCESS" {
		t.Fatal(r, e)
	}
	if e := p.Close(context.Background(), QueryRequest{TradeNo: "ORDER_1"}); e != nil {
		t.Fatal(e)
	}
	badIdentity = true
	if _, e = p.Query(context.Background(), QueryRequest{TradeNo: "ORDER_1"}); e == nil {
		t.Fatal("foreign merchant response accepted")
	}
	if _, e = p.QueryRefund(context.Background(), refund); e == nil {
		t.Fatal("changed refund amount accepted")
	}
	var unknown *UnknownOutcome
	if _, e = p.Refund(context.Background(), refund); !errors.As(e, &unknown) {
		t.Fatal("bad financial response is not unknown", e)
	}
	before := calls
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "ORDER_1", Subject: "test", Method: "scan", Amount: 101}); e == nil || calls != before {
		t.Fatal("unsupported method reached upstream")
	}
}

func TestWechatV3NotificationRequiresSignedEnvelopeAndBoundEncryptedTransaction(t *testing.T) {
	merchant := testKey(t)
	platform := testKey(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	p, e := NewWechatV3(WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchant, APIv3Key: key, VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL": &platform.PublicKey}})
	if e != nil {
		t.Fatal(e)
	}
	expected := ExpectedPayment{TradeNo: "ORDER_1", Amount: 101}
	for _, tc := range []struct {
		name, merchant, event string
		amount                int64
		duplicate             bool
	}{{"valid", "local-mch", "TRANSACTION.SUCCESS", 101, false}, {"wrong-account", "foreign", "TRANSACTION.SUCCESS", 101, false}, {"wrong-amount", "local-mch", "TRANSACTION.SUCCESS", 102, false}, {"wrong-event", "local-mch", "REFUND.SUCCESS", 101, false}, {"duplicate-amount", "local-mch", "TRANSACTION.SUCCESS", 101, true}} {
		t.Run(tc.name, func(t *testing.T) {
			transaction := `{"appid":"local-app","mchid":"` + tc.merchant + `","out_trade_no":"ORDER_1","transaction_id":"WX_ORIGINAL","trade_state":"SUCCESS","amount":{"total":` + strconv.FormatInt(tc.amount, 10) + `,"currency":"CNY"},"payer":{"openid":"trusted-buyer"}}`
			if tc.duplicate {
				transaction = strings.Replace(transaction, `"total":101`, `"total":100,"total":101`, 1)
			}
			block, _ := aes.NewCipher(key)
			gcm, _ := cipher.NewGCM(block)
			n := []byte("0123456789ab")
			encrypted := gcm.Seal(nil, n, []byte(transaction), []byte("transaction"))
			body, _ := json.Marshal(map[string]any{"id": "evt-local", "create_time": time.Now().Format(time.RFC3339), "event_type": tc.event, "resource_type": "encrypt-resource", "resource": map[string]string{"algorithm": "AEAD_AES_256_GCM", "original_type": "transaction", "nonce": string(n), "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(encrypted)}})
			recorder := httptest.NewRecorder()
			v3SignedResponse(t, recorder, platform, 200, string(body), "PUB_KEY_ID_LOCAL")
			event, e := p.VerifyNotification(context.Background(), recorder.Header(), body, expected)
			if tc.name == "valid" {
				if e != nil || event.Amount != 101 || event.EventID != "evt-local" {
					t.Fatal(event, e)
				}
				if _, e = p.VerifyNotification(context.Background(), recorder.Header(), append(body, ' '), expected); e == nil {
					t.Fatal("unsigned mutation accepted")
				}
			} else if e == nil {
				t.Fatal("untrusted callback accepted")
			}
		})
	}
}
