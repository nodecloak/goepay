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
	"errors"
	adapter "goepay/internal/adapters/payment"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuyerCharge104WechatTLSActualWireAEADCallbackAndRefund(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 10412, 600, "platform")
	buyer104PolicyFixture(t, s, "merchant", 10412, 1, nil, nil)
	o, e := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: 10412, OutTradeNo: "wechat-actual104", Name: "local104", Type: "wxpay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	merchant, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	var creates, refunds atomic.Int32
	var wrong atomic.Bool
	headers := func(raw []byte) http.Header {
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := "local104-message"
		hash := sha256.Sum256([]byte(stamp + "\n" + nonce + "\n" + string(raw) + "\n"))
		sig, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, hash[:])
		if e != nil {
			t.Error(e)
		}
		h := http.Header{}
		h.Set("Wechatpay-Timestamp", stamp)
		h.Set("Wechatpay-Nonce", nonce)
		h.Set("Wechatpay-Serial", "PUB_KEY_ID_LOCAL104")
		h.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(sig))
		return h
	}
	transaction := func(amount int64) map[string]any {
		return map[string]any{"appid": "local104-app", "mchid": "local104-mch", "out_trade_no": o.TradeNo, "transaction_id": "WX104_" + o.TradeNo, "trade_state": "SUCCESS", "amount": map[string]any{"total": amount, "currency": "CNY"}, "payer": map[string]string{"openid": "buyer104"}}
	}
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		p := map[string]string{}
		for _, part := range strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "WECHATPAY2-SHA256-RSA2048 "), ",") {
			k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				t.Error("invalid local Wechat auth")
				w.WriteHeader(400)
				return
			}
			p[k], e = strconv.Unquote(v)
			if e != nil {
				t.Error(e)
			}
		}
		message, e := adapter.WechatV3RequestMessage(r.Method, r.URL.RequestURI(), p["timestamp"], p["nonce_str"], raw)
		hash := sha256.Sum256(message)
		sig, se := base64.StdEncoding.DecodeString(p["signature"])
		if e != nil || se != nil || rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, hash[:], sig) != nil {
			t.Error("Wechat actual request RSA invalid")
			w.WriteHeader(400)
			return
		}
		var out any
		switch {
		case r.Method == "POST" && r.URL.Path == "/v3/pay/transactions/native":
			creates.Add(1)
			var in struct {
				Trade  string                `json:"out_trade_no"`
				Amount struct{ Total int64 } `json:"amount"`
			}
			if json.Unmarshal(raw, &in) != nil || in.Trade != o.TradeNo || in.Amount.Total != 106 {
				t.Error("wechat provider charge is not actual106", in)
			}
			out = map[string]any{"code_url": "weixin://wxpay/bizpayurl?pr=local104"}
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v3/pay/transactions/out-trade-no/"):
			amount := int64(106)
			if wrong.Load() {
				amount = 100
			}
			out = transaction(amount)
		case r.Method == "POST" && r.URL.Path == "/v3/refund/domestic/refunds":
			refunds.Add(1)
			var in struct {
				Trade  string `json:"out_trade_no"`
				No     string `json:"out_refund_no"`
				Amount struct {
					Total, Refund int64
					Currency      string
				} `json:"amount"`
			}
			if json.Unmarshal(raw, &in) != nil || in.Trade != o.TradeNo || in.Amount.Total != 106 || in.Amount.Refund != 106 || in.Amount.Currency != "CNY" || !strings.HasPrefix(in.No, "RF") {
				t.Error("wechat actual full refund mismatch", in)
			}
			out = map[string]any{"out_trade_no": o.TradeNo, "transaction_id": "WX104_" + o.TradeNo, "out_refund_no": in.No, "refund_id": "WX104_" + in.No, "status": "SUCCESS", "amount": map[string]any{"total": 106, "refund": 106, "currency": "CNY"}}
		default:
			t.Error("unexpected local104 endpoint", r.Method, r.URL.Path)
			w.WriteHeader(400)
			return
		}
		encoded, _ := json.Marshal(out)
		for k, v := range headers(encoded) {
			w.Header()[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(encoded)
	}))
	defer up.Close()
	p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local104-app", MerchantID: "local104-mch", MerchantSerial: "LOCAL104", PrivateKey: merchant, APIv3Key: key, VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL104": &platform.PublicKey}, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: sharingReceiverTLSTransport{up}}})
	if e != nil {
		t.Fatal(e)
	}
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, LocalSimulation: true, CallbackBaseURL: "https://local.invalid"}
	if _, e = flow.Submit(t.Context(), o.TradeNo, 7, "native", "192.0.2.104", ""); e != nil {
		t.Fatal(e)
	}
	wrong.Store(true)
	if _, e = flow.Query(t.Context(), 10412, o.TradeNo); !errors.Is(e, adapter.ErrAuthentication) {
		t.Fatal("signed M query settled C", e)
	}
	callback := func(amount int64) (http.Header, []byte) {
		plain, _ := json.Marshal(transaction(amount))
		b, _ := aes.NewCipher(key)
		g, _ := cipher.NewGCM(b)
		nonce := "local104aead"
		encrypted := g.Seal(nil, []byte(nonce), plain, []byte("transaction"))
		raw, _ := json.Marshal(map[string]any{"id": "notify104", "event_type": "TRANSACTION.SUCCESS", "resource_type": "encrypt-resource", "resource": map[string]string{"original_type": "transaction", "algorithm": "AEAD_AES_256_GCM", "nonce": nonce, "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(encrypted)}})
		return headers(raw), raw
	}
	h, raw := callback(100)
	if e = flow.HandleNotification(t.Context(), 7, o.TradeNo, h, raw); !errors.Is(e, adapter.ErrAuthentication) {
		t.Fatal("AEAD M callback settled C", e)
	}
	h, raw = callback(106)
	forged := h.Clone()
	forged.Set("Wechatpay-Signature", "forged")
	if e = flow.HandleNotification(t.Context(), 7, o.TradeNo, forged, raw); !errors.Is(e, adapter.ErrAuthentication) {
		t.Fatal("unsigned C accepted", e)
	}
	if e = flow.HandleNotification(t.Context(), 7, o.TradeNo, h, raw); e != nil {
		t.Fatal("signed AEAD C failed", e)
	}
	if e = flow.HandleNotification(t.Context(), 7, o.TradeNo, h, raw); e != nil {
		t.Fatal("callback replay", e)
	}
	wrong.Store(false)
	if _, e = flow.Query(t.Context(), 10412, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	if r, e := flow.Refund(t.Context(), 10412, o.TradeNo, "wechat-full104", 106); e != nil || r.Status != "succeeded" {
		t.Fatal(r, e)
	}
	if creates.Load() != 1 || refunds.Load() != 1 {
		t.Fatal("replayed real adapter send", creates.Load(), refunds.Load())
	}
}
