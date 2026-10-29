package contract_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

type v3ContractTLS struct{ up *httptest.Server }

func (x v3ContractTLS) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.mch.weixin.qq.com" {
		return nil, adapter.ErrAuthentication
	}
	copy := r.Clone(r.Context())
	target, _ := url.Parse(x.up.URL)
	copy.URL.Scheme = target.Scheme
	copy.URL.Host = target.Host
	return x.up.Client().Transport.RoundTrip(copy)
}
func v3ContractHeaders(t *testing.T, body []byte, key *rsa.PrivateKey) http.Header {
	t.Helper()
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	n := "local-http-nonce"
	hash := sha256.Sum256([]byte(stamp + "\n" + n + "\n" + string(body) + "\n"))
	signature, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if e != nil {
		t.Fatal(e)
	}
	h := http.Header{}
	h.Set("Wechatpay-Timestamp", stamp)
	h.Set("Wechatpay-Nonce", n)
	h.Set("Wechatpay-Serial", "PUB_KEY_ID_HTTP")
	h.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(signature))
	return h
}
func TestWechatV3HTTPActualAdapterCreditsOnceAndAcknowledgesJSONProtocol(t *testing.T) {
	s, pool, key := fixture(t)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	apiKey := []byte("0123456789abcdef0123456789abcdef")
	trade, refund := "", ""
	creates := 0
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw []byte
		switch {
		case r.URL.Path == "/v3/pay/transactions/native":
			creates++
			var request struct {
				OutTradeNo string `json:"out_trade_no"`
			}
			json.NewDecoder(r.Body).Decode(&request)
			trade = request.OutTradeNo
			raw = []byte(`{"code_url":"weixin://wxpay/bizpayurl?pr=local"}`)
		case r.URL.Path == "/v3/refund/domestic/refunds" || strings.HasPrefix(r.URL.Path, "/v3/refund/domestic/refunds/"):
			state := "SUCCESS"
			if r.Method == "POST" {
				state = "PROCESSING"
				var request struct {
					No string `json:"out_refund_no"`
				}
				json.NewDecoder(r.Body).Decode(&request)
				refund = request.No
			}
			raw, _ = json.Marshal(map[string]any{"out_trade_no": trade, "transaction_id": "WX_ORIGINAL", "out_refund_no": refund, "refund_id": "WX_REFUND", "status": state, "amount": map[string]any{"total": 101, "refund": 40, "currency": "CNY"}})
		default:
			t.Error("unexpected real adapter path", r.URL.Path)
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
	p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: key, APIv3Key: apiKey, VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_HTTP": &platform.PublicKey}, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: v3ContractTLS{up}}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	s.CheckoutKey = bytes.Repeat([]byte{7}, 32)
	params := compat.Params{"pid": "1", "out_trade_no": "v3-http-local", "money": "1.01", "name": "local", "type": "wxpay", "method": "web", "channel_id": "7", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return"}
	_, out := call(t, s.Handler(), "POST", "/mapi.php", signed(params, "contract-secret"))
	if out["code"] != float64(1) || trade == "" || creates != 1 {
		t.Fatal(out, creates)
	}
	callback := func(mch string) *httptest.ResponseRecorder {
		transaction, _ := json.Marshal(map[string]any{"appid": "local-app", "mchid": mch, "out_trade_no": trade, "transaction_id": "WX_ORIGINAL", "trade_state": "SUCCESS", "amount": map[string]any{"total": 101, "currency": "CNY"}, "payer": map[string]string{"openid": "local-buyer"}})
		block, _ := aes.NewCipher(apiKey)
		gcm, _ := cipher.NewGCM(block)
		nonce := []byte("0123456789ab")
		encrypted := gcm.Seal(nil, nonce, transaction, []byte("transaction"))
		body, _ := json.Marshal(map[string]any{"id": "HTTP_EVENT_1", "event_type": "TRANSACTION.SUCCESS", "resource_type": "encrypt-resource", "resource": map[string]string{"original_type": "transaction", "algorithm": "AEAD_AES_256_GCM", "nonce": string(nonce), "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(encrypted)}})
		request := httptest.NewRequest("POST", "/callbacks/providers/7/"+trade, bytes.NewReader(body))
		request.Header = v3ContractHeaders(t, body, platform)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, request)
		return w
	}
	if w := callback("foreign-mch"); w.Code != 400 || !strings.Contains(w.Header().Get("Content-Type"), "json") {
		t.Fatal("V3 invalid notification must reject in JSON protocol", w.Code, w.Body.String())
	}
	for i := 0; i < 2; i++ {
		w := callback("local-mch")
		if w.Code != 204 || w.Body.Len() != 0 {
			t.Fatal("V3 success must acknowledge 204 without an XML body", w.Code, w.Body.String())
		}
	}
	ctx := context.Background()
	var credits int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM journals WHERE event_key=$1`, "payment:"+trade).Scan(&credits); e != nil || credits != 1 {
		t.Fatal("callback ledger idempotency", credits, e)
	}
	r, e := s.ProviderFlow.Refund(ctx, 1, trade, "v3-refund-local", 40)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProviderFlow.QueryRefund(ctx, 1, r.RefundNo); e != nil {
		t.Fatal(e)
	}
	balances, e := s.Store.Balance(ctx, 1)
	if e != nil || balances.Available != 61 || balances.Reserved != 0 {
		t.Fatal(balances, e)
	}
	_, out = call(t, s.Handler(), "POST", "/mapi.php", signed(params, "contract-secret"))
	if creates != 1 {
		t.Fatal("callback replay caused another provider create", creates, out)
	}
}
