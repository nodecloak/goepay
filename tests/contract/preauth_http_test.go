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

func TestPreauthHTTPFreezeUnknownCaptureStatusFourAndReadOnlyRecovery(t *testing.T) {
	s, pool, merchant := fixture(t)
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	captures, queries := 0, 0
	confirmed := false
	sign := func(raw string) string {
		digest := sha256.Sum256([]byte(raw))
		signature, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		if e != nil {
			t.Fatal(e)
		}
		return base64.StdEncoding.EncodeToString(signature)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		method := r.Form.Get("method")
		var biz map[string]any
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
		if method == "alipay.trade.pay" {
			captures++
			if biz["auth_no"] != "authorization-http" || biz["auth_confirm_mode"] != "COMPLETE" || biz["total_amount"] != "1.25" {
				t.Error(biz)
			}
			w.WriteHeader(503)
			return
		}
		if method != "alipay.trade.query" {
			t.Error("unexpected method", method)
			w.WriteHeader(500)
			return
		}
		queries++
		state := "WAIT_BUYER_PAY"
		if confirmed {
			state = "TRADE_SUCCESS"
		}
		node, _ := json.Marshal(map[string]any{"code": "10000", "out_trade_no": biz["out_trade_no"], "trade_no": "payment-http", "trade_status": state, "total_amount": "1.25"})
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_query_response": json.RawMessage(node), "sign": sign(string(node))})
	}))
	defer upstream.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", SellerID: "seller", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{8}, 32)
	s.MerchantChannels = map[int64][]int64{2: {7}}
	h := s.Handler()
	invoke := func(path string, p compat.Params) map[string]any {
		p["pid"] = "2"
		p["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
		p["sign_type"] = "RSA"
		p["sign"], _ = compat.SignRSA(p, merchant)
		v := url.Values{}
		for k, val := range p {
			v.Set(k, val)
		}
		_, out := call(t, h, "POST", path, v)
		return out
	}
	out := invoke("/api/pay/create", compat.Params{"out_trade_no": "preauth-http", "money": "1.25", "name": "deposit", "type": "alipay", "method": "preauth", "channel_id": "7", "clientip": "192.0.2.1"})
	if out["code"] != float64(0) || out["pay_type"] != "jsapi" {
		t.Fatal("preauth SDK creation unavailable", out)
	}
	sdk, e := url.ParseQuery(out["pay_info"].(string))
	if e != nil || sdk.Get("method") != "alipay.fund.auth.order.app.freeze" {
		t.Fatal("wrong SDK material", sdk, e)
	}
	if captures != 0 || queries != 0 {
		t.Fatal("SDK creation called upstream")
	}
	trade := out["trade_no"].(string)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/pay/preauth/"+trade+"/", nil))
	if w.Code != 403 {
		t.Fatal("guessable authorization exposed", w.Code)
	}
	notification := map[string]string{"app_id": "app", "payee_user_id": "seller", "out_order_no": trade, "out_request_no": trade, "auth_no": "authorization-http", "payer_user_id": "2088000000000000", "amount": "1.25", "status": "SUCCESS", "operation_type": "FREEZE", "sign_type": "RSA2"}
	notification["sign"] = sign(adapter.AlipayNotificationCanonical(notification))
	form := url.Values{}
	for k, val := range notification {
		form.Set(k, val)
	}
	for i := 0; i < 3; i++ {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/pay/preauthnotify/"+trade+"/", strings.NewReader(form.Encode())))
		if w.Code != 200 || w.Body.String() != "success" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	b, _ := s.Store.Balance(context.Background(), 2)
	if b.Available != 0 || captures != 1 {
		t.Fatal("authorization/unknown credited or resent", b, captures)
	}
	out = invoke("/api/pay/query", compat.Params{"trade_no": trade})
	if out["status"] != float64(4) || out["api_trade_no"] != "authorization-http" {
		t.Fatal("authorization status lost", out)
	}
	confirmed = true
	s.ProviderFlow.AllowLive = false
	out = invoke("/api/pay/query", compat.Params{"trade_no": trade})
	if out["status"] != float64(1) || out["api_trade_no"] != "payment-http" {
		t.Fatal("readonly capture recovery failed", out)
	}
	b, _ = s.Store.Balance(context.Background(), 2)
	var state string
	e = pool.QueryRow(context.Background(), "SELECT a.capture_state FROM payment_authorizations a JOIN orders o ON o.id=a.order_id WHERE o.trade_no=$1", trade).Scan(&state)
	if e != nil || b.Available != 125 || captures != 1 || state != "succeeded" {
		t.Fatal(b, captures, state, e)
	}
}
