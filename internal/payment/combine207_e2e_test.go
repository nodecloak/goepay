package payment

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
)

// One combined order end to end through ProviderFlow and the real WeChat v3
// adapter against an in-process signed TLS double: settings on -> sub-orders
// recorded and one combined create sent -> verified combined callback marks
// the parent and every sub-order paid -> a refund is split per sub-order.
func TestCombine207EndToEndCreateNotifyRefund(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	if err := s.CreateMerchant(ctx, Merchant{ID: 920208, Name: "combine207-e2e", APIKey: "test-only"}); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"wxcombine_open": "1", "wxcombine_minmoney": "100", "wxcombine_submoney": "50"} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, k, v); err != nil {
			t.Fatal(err)
		}
	}
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 920208, OutTradeNo: "combine207-e2e", Name: "合单", Type: "wxpay", Amount: 20000})
	if err != nil {
		t.Fatal(err)
	}
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	key := []byte("0123456789abcdef0123456789abcdef")
	sign := func(raw []byte) http.Header {
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		hash := sha256.Sum256([]byte(stamp + "\nlocal207\n" + string(raw) + "\n"))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, hash[:])
		h := http.Header{}
		h.Set("Wechatpay-Timestamp", stamp)
		h.Set("Wechatpay-Nonce", "local207")
		h.Set("Wechatpay-Serial", "PUB_KEY_ID_LOCAL207")
		h.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(sig))
		return h
	}
	var mu sync.Mutex
	var creates, ordinary int
	var subNos []string
	var refunds []map[string]any
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		var out any
		switch {
		case r.Method == "POST" && r.URL.Path == "/v3/combine-transactions/native":
			creates++
			var in struct {
				No   string `json:"combine_out_trade_no"`
				Subs []struct {
					No     string `json:"out_trade_no"`
					Amount struct {
						Total int64 `json:"total_amount"`
					} `json:"amount"`
				} `json:"sub_orders"`
			}
			if json.Unmarshal(raw, &in) != nil || in.No != o.TradeNo || len(in.Subs) != 4 {
				t.Error("combined create body wrong", string(raw))
			}
			for _, s := range in.Subs {
				subNos = append(subNos, s.No)
				if s.Amount.Total != 5000 {
					t.Error("sub amount wrong", s)
				}
			}
			out = map[string]any{"code_url": "weixin://wxpay/bizpayurl?pr=combine207"}
		case strings.HasPrefix(r.URL.Path, "/v3/pay/transactions/"):
			ordinary++
			out = map[string]any{}
		case r.Method == "POST" && r.URL.Path == "/v3/refund/domestic/refunds":
			var in map[string]any
			json.Unmarshal(raw, &in)
			refunds = append(refunds, in)
			amount := in["amount"].(map[string]any)
			out = map[string]any{"out_trade_no": in["out_trade_no"], "transaction_id": "WX207_" + in["out_trade_no"].(string), "out_refund_no": in["out_refund_no"], "refund_id": "WXR_" + in["out_refund_no"].(string), "status": "SUCCESS", "amount": map[string]any{"total": amount["total"], "refund": amount["refund"], "currency": "CNY"}}
		default:
			t.Error("unexpected endpoint", r.Method, r.URL.Path)
			w.WriteHeader(400)
			return
		}
		encoded, _ := json.Marshal(out)
		for k, v := range sign(encoded) {
			w.Header()[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(encoded)
	}))
	defer up.Close()
	p, err := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local207-app", MerchantID: "local207-mch", MerchantSerial: "LOCAL207", PrivateKey: merchant, APIv3Key: key, VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL207": &platform.PublicKey}, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: sharingReceiverTLSTransport{up}}})
	if err != nil {
		t.Fatal(err)
	}
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, LocalSimulation: true, CallbackBaseURL: "https://local.invalid"}
	attempt, err := flow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.207", "")
	if err != nil || attempt.Material != "weixin://wxpay/bizpayurl?pr=combine207" {
		t.Fatal("combined submit failed", attempt, err)
	}
	subs, _ := s.CombineSubOrders(ctx, o.ID)
	if creates != 1 || ordinary != 0 || len(subs) != 4 || subs[0].SubTradeNo != subNos[0] || subs[3].SubTradeNo != o.TradeNo+"4" {
		t.Fatal("combined create not recorded/sent once", creates, ordinary, subs, subNos)
	}
	// Signed, encrypted combined callback.
	subOrders := []map[string]any{}
	for i, sub := range subs {
		subOrders = append(subOrders, map[string]any{"mchid": "local207-mch", "trade_state": "SUCCESS", "transaction_id": "WX207_" + sub.SubTradeNo, "out_trade_no": sub.SubTradeNo, "amount": map[string]any{"total_amount": sub.Amount, "currency": "CNY"}, "attach": "combine", "trade_type": "NATIVE", "position": i})
	}
	plain, _ := json.Marshal(map[string]any{"combine_appid": "local207-app", "combine_mchid": "local207-mch", "combine_out_trade_no": o.TradeNo, "sub_orders": subOrders, "combine_payer_info": map[string]string{"openid": "buyer207"}})
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	encrypted := gcm.Seal(nil, []byte("local207aead"), plain, []byte("transaction"))
	body, _ := json.Marshal(map[string]any{"id": "notify207", "event_type": "TRANSACTION.SUCCESS", "resource_type": "encrypt-resource", "resource": map[string]string{"original_type": "transaction", "algorithm": "AEAD_AES_256_GCM", "nonce": "local207aead", "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(encrypted)}})
	if err = flow.HandleNotification(ctx, 7, o.TradeNo, sign(body), body); err != nil {
		t.Fatal("verified combined callback rejected", err)
	}
	if err = flow.HandleNotification(ctx, 7, o.TradeNo, sign(body), body); err != nil {
		t.Fatal("combined callback replay not idempotent", err)
	}
	paid, _ := s.OrderByTradeNo(ctx, o.TradeNo)
	subs, _ = s.CombineSubOrders(ctx, o.ID)
	if paid.Status != "paid" {
		t.Fatal("parent not paid", paid.Status)
	}
	for _, sub := range subs {
		if sub.Status != "paid" || sub.TransactionID != "WX207_"+sub.SubTradeNo {
			t.Fatal("sub-order not paid with its own transaction", sub)
		}
	}
	// A refund of 120.00 spans sub-orders 1, 2 and part of 3.
	r, err := flow.Refund(ctx, 920208, o.TradeNo, "combine207-refund", 12000)
	if err != nil || r.Status != "succeeded" {
		t.Fatal("combined refund failed", r, err)
	}
	if len(refunds) != 3 || refunds[0]["out_trade_no"] != subs[0].SubTradeNo || refunds[2]["out_trade_no"] != subs[2].SubTradeNo || refunds[2]["amount"].(map[string]any)["refund"] != float64(2000) || !strings.HasSuffix(refunds[0]["out_refund_no"].(string), "-1") {
		t.Fatal("refund not split per sub-order with fixed numbers", refunds)
	}
}
