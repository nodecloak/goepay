package contract_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"

	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"

	"testing"
)

// Exercise balances using the real V3 adapter and authenticated encrypted
// callbacks. Every cent has an original payment receipt and balanced journals.
func TestMerchant167ChannelSharingFundingJournalReferenceUsesActualFK(t *testing.T) {
	s, pool, merchantKey := fixture(t)
	ctx := context.Background()
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	apiKey := []byte("0123456789abcdef0123456789abcdef")
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input map[string]json.RawMessage
		if e := json.NewDecoder(r.Body).Decode(&input); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		var raw []byte
		switch r.URL.Path {
		case "/v3/profitsharing/receivers/add":
			raw, _ = json.Marshal(map[string]json.RawMessage{"type": input["type"], "account": input["account"], "relation_type": input["relation_type"]})
		case "/v3/pay/transactions/native":
			raw = []byte(`{"code_url":"weixin://wxpay/bizpayurl?pr=local-balance"}`)
		default:
			t.Error("unexpected local adapter request", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		for name, values := range v3ContractHeaders(t, raw, platform) {
			w.Header()[name] = values
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}))
	defer up.Close()
	p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchantKey, APIv3Key: apiKey, VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_HTTP": &platform.PublicKey}, EnableChannelSharing: true, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: v3ContractTLS{up}}})
	if e != nil {
		t.Fatal(e)
	}
	g := &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.ProviderFlow = g
	for _, mid := range []int64{1, 2} {
		actor := payment.TransferBatchActor{Role: "merchant", ID: mid, Name: fmt.Sprintf("local-%d", mid)}
		receiver, e := g.CreateChannelSharingReceiver(ctx, mid, 7, fmt.Sprintf("balance-receiver-%d", mid), adapter.SharingReceiver{Type: "MERCHANT_ID", Account: fmt.Sprintf("local-receiver-%d", mid), Name: "Local Receiver", RelationType: "SUPPLIER"}, actor)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = g.BindChannelSharingReceiver(ctx, mid, receiver.ID, fmt.Sprintf("balance-bind-%d", mid), actor); e != nil {
			t.Fatal(e)
		}
		if _, e = g.SetChannelSharingRule(ctx, mid, 7, payment.ChannelSharingRuleInput{Enabled: true, MinimumAmount: 100, Methods: []string{"native"}, Items: []payment.ChannelSharingRuleItem{{ReceiverID: receiver.ID, Mode: "rate", RateBPS: 1000}}}, 0, actor); e != nil {
			t.Fatal(e)
		}
	}
	for _, item := range []struct{ mid, amount int64 }{{1, 101}, {2, 203}, {1, 17}} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: item.mid, OutTradeNo: fmt.Sprintf("balance-paid-%d-%d", item.mid, item.amount), Type: "wxpay", Amount: item.amount, Name: "local balance item"})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
			t.Fatal(e)
		}
		transaction, _ := json.Marshal(map[string]any{"appid": "local-app", "mchid": "local-mch", "out_trade_no": o.TradeNo, "transaction_id": "WX_BALANCE_" + o.TradeNo, "trade_state": "SUCCESS", "amount": map[string]any{"total": item.amount, "currency": "CNY"}, "payer": map[string]string{"openid": "local-balance-buyer"}})
		block, _ := aes.NewCipher(apiKey)
		gcm, _ := cipher.NewGCM(block)
		nonce := []byte("0123456789ab")
		encrypted := gcm.Seal(nil, nonce, transaction, []byte("transaction"))
		body, _ := json.Marshal(map[string]any{"id": "BALANCE_EVENT_" + o.TradeNo, "event_type": "TRANSACTION.SUCCESS", "resource_type": "encrypt-resource", "resource": map[string]string{"original_type": "transaction", "algorithm": "AEAD_AES_256_GCM", "nonce": string(nonce), "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(encrypted)}})
		request := httptest.NewRequest("POST", "/callbacks/providers/7/"+o.TradeNo, bytes.NewReader(body))
		request.Header = v3ContractHeaders(t, body, platform)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, request)
		if w.Code != http.StatusNoContent {
			t.Fatal("authenticated local callback rejected", w.Code, w.Body.String())
		}
	}

	rows, e := pool.Query(ctx, "SELECT o.trade_no,f.reserve_journal_id FROM channel_sharing_fundings f JOIN orders o ON o.id=f.order_id ORDER BY o.id")
	if e != nil {
		t.Fatal(e)
	}
	type link struct {
		trade   string
		journal int64
	}
	var links []link
	for rows.Next() {
		var v link
		if e = rows.Scan(&v.trade, &v.journal); e != nil {
			t.Fatal(e)
		}
		links = append(links, v)
	}
	rows.Close()
	if len(links) != 2 {
		t.Fatal(links)
	}
	for i, v := range links {
		var paymentID int64
		if e = pool.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", "payment:"+v.trade).Scan(&paymentID); e != nil {
			t.Fatal(e)
		}
		owner := int64(i + 1)
		merchant167CheckLedger(t, s, pool, owner, v.trade, []int64{paymentID, v.journal})
		merchant167CheckLedger(t, s, pool, 0, v.trade, []int64{paymentID, v.journal})
		merchant167CheckLedger(t, s, pool, 3-owner, v.trade, nil)
	}
}
