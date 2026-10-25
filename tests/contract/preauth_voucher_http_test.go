package contract_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPreauthVoucherHTTPIsNotPaymentAndNeverResent(t *testing.T) {
	s, pool, key := fixture(t)
	var calls atomic.Int64
	var unavailable atomic.Bool
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if unavailable.Load() {
			w.WriteHeader(503)
			return
		}
		if e := r.ParseForm(); e != nil {
			t.Error(e)
		}
		var biz map[string]any
		if e := json.Unmarshal([]byte(r.PostForm.Get("biz_content")), &biz); e != nil {
			t.Error(e)
		}
		if r.PostForm.Get("method") != "alipay.fund.auth.order.voucher.create" {
			t.Error("non-voucher operation sent")
		}
		raw, _ := json.Marshal(map[string]any{"code": "10000", "out_order_no": biz["out_order_no"], "out_request_no": biz["out_request_no"], "code_type": "qrCode", "code_value": "https://qr.alipay.com/local-authorization"})
		digest := sha256.Sum256(raw)
		signature, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if e != nil {
			t.Error(e)
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_fund_auth_order_voucher_create_response": json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signature)})
	}))
	defer upstream.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"preauth"}, PreauthVoucher: true, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{9: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{9}, 32)
	s.MerchantChannels = map[int64][]int64{2: {9}}
	p := compat.Params{"pid": "2", "type": "alipay", "method": "jump", "channel_id": "9", "out_trade_no": "voucher-order", "name": "authorization", "money": "1.01", "sign_type": "RSA", "timestamp": fmt.Sprint(time.Now().Unix())}
	p["sign"], _ = compat.SignRSA(p, key)
	values := url.Values{}
	for k, v := range p {
		values.Set(k, v)
	}
	var trade, checkout string
	for i := 0; i < 2; i++ {
		_, out := call(t, s.Handler(), "POST", "/api/pay/create", values)
		if out["code"] != float64(0) || out["pay_type"] != "jump" {
			t.Fatal(out)
		}
		if i == 0 {
			trade = out["trade_no"].(string)
			checkout = out["pay_info"].(string)
		} else if out["trade_no"] != trade {
			t.Fatal("voucher retry replaced order")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("repeated voucher created %d upstream intents", calls.Load())
	}
	attempt, e := s.Store.ProviderAttempt(context.Background(), trade)
	if e != nil || attempt.Method != "preauth" || attempt.MaterialType != "qrcode" || attempt.ProviderTradeNo != "" {
		t.Fatalf("voucher attempt=%+v err=%v", attempt, e)
	}
	balance, e := s.Store.Balance(context.Background(), 2)
	if e != nil || balance.Available != 0 || balance.Reserved != 0 {
		t.Fatalf("voucher credited merchant %+v %v", balance, e)
	}
	var journals, authorizations int
	if e = pool.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM journals),(SELECT count(*) FROM payment_authorizations)").Scan(&journals, &authorizations); e != nil || journals != 0 || authorizations != 0 {
		t.Fatalf("voucher was accepted as authorization/payment %d/%d %v", journals, authorizations, e)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", checkout, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "授权二维码") || !strings.Contains(w.Body.String(), "实际扣款成功后才入账") {
		t.Fatalf("authorization checkout status=%d", w.Code)
	}
	u, _ := url.Parse(checkout)
	u.Path += "/qr.png"
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", u.String(), nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("authorization QR status=%d", w.Code)
	}
	unavailable.Store(true)
	p["out_trade_no"] = "voucher-unknown"
	p["sign"], _ = compat.SignRSA(p, key)
	for k, v := range p {
		values.Set(k, v)
	}
	_, out := call(t, s.Handler(), "POST", "/api/pay/create", values)
	if out["code"] != float64(-1) {
		t.Fatal("unknown voucher declared ready", out)
	}
	unavailable.Store(false)
	_, out = call(t, s.Handler(), "POST", "/api/pay/create", values)
	if out["code"] != float64(-1) || calls.Load() != 2 {
		t.Fatalf("unknown voucher was resubmitted calls=%d result=%v", calls.Load(), out)
	}
	unknown, e := s.Store.Order(context.Background(), 2, "voucher-unknown")
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.Store.ProviderAttempt(context.Background(), unknown.TradeNo)
	if e != nil || a.State != "unknown" || a.Material != "" {
		t.Fatalf("unknown voucher evidence=%+v err=%v", a, e)
	}
}
