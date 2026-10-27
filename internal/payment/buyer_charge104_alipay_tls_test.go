package payment

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBuyerCharge104AlipayTLSActualWireSignedQueryRefundAndOriginalNotify(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 10411, 600, "platform")
	buyer104PolicyFixture(t, s, "merchant", 10411, 1, nil, nil)
	o, e := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: 10411, OutTradeNo: "alipay-actual104", Name: "local104", Type: "alipay", Amount: 100})
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
	var creates, refunds, queries atomic.Int32
	var wrong atomic.Bool
	sign := func(raw []byte) string {
		hash := sha256.Sum256(raw)
		b, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, hash[:])
		if e != nil {
			t.Error(e)
		}
		return base64.StdEncoding.EncodeToString(b)
	}
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		p := map[string]string{}
		for k, v := range r.Form {
			if len(v) != 1 {
				t.Error("duplicate actual wire field")
				w.WriteHeader(400)
				return
			}
			p[k] = v[0]
		}
		hash := sha256.Sum256([]byte(adapter.AlipayRequestCanonical(p)))
		sig, e := base64.StdEncoding.DecodeString(p["sign"])
		if e != nil || rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, hash[:], sig) != nil {
			t.Error("actual Alipay request RSA invalid")
			w.WriteHeader(400)
			return
		}
		var b map[string]any
		if json.Unmarshal([]byte(p["biz_content"]), &b) != nil || b["out_trade_no"] != o.TradeNo {
			t.Error("lost original payment identity")
			w.WriteHeader(400)
			return
		}
		out := map[string]any{"code": "10000", "out_trade_no": o.TradeNo, "trade_no": "ALI104_" + o.TradeNo}
		switch p["method"] {
		case "alipay.trade.precreate":
			creates.Add(1)
			if b["total_amount"] != "1.06" {
				t.Errorf("provider charged M instead of C: %q", b["total_amount"])
			}
			out["qr_code"] = "https://local.invalid/104"
		case "alipay.trade.query":
			out["total_amount"] = "1.06"
			if wrong.Load() {
				out["total_amount"] = "1.00"
			}
			out["trade_status"] = "TRADE_SUCCESS"
			out["buyer_user_id"] = "buyer104"
		case "alipay.trade.refund":
			refunds.Add(1)
			if b["refund_amount"] != "1.06" || !strings.HasPrefix(fmt.Sprint(b["out_request_no"]), "RF") {
				t.Error("actual full refund lost", b)
			}
			w.WriteHeader(503)
			return
		case "alipay.trade.fastpay.refund.query":
			queries.Add(1)
			out["out_request_no"] = b["out_request_no"]
			out["refund_amount"] = "1.06"
			out["refund_status"] = "REFUND_SUCCESS"
		default:
			t.Error("unexpected local104 method", p["method"])
			w.WriteHeader(400)
			return
		}
		raw, _ := json.Marshal(out)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(p["method"], ".", "_") + "_response": json.RawMessage(raw), "sign": sign(raw)})
	}))
	defer up.Close()
	p, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "local104-app", SellerID: "local104-seller", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client(), Methods: []string{"native"}})
	if e != nil {
		t.Fatal(e)
	}
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, LocalSimulation: true, CallbackBaseURL: "https://local.invalid"}
	if _, e = flow.Submit(t.Context(), o.TradeNo, 7, "native", "192.0.2.104", ""); e != nil {
		t.Fatal("actual-wire submit", e)
	}
	wrong.Store(true)
	if _, e = flow.Query(t.Context(), 10411, o.TradeNo); !errors.Is(e, adapter.ErrAuthentication) {
		t.Fatal("signed original M incorrectly settled C", e)
	}
	var inbox int
	if e = s.pool.QueryRow(t.Context(), `SELECT count(*) FROM inbox`).Scan(&inbox); e != nil || inbox != 0 {
		t.Fatal("wrong amount side effects", inbox, e)
	}
	callback := func(amount string) []byte {
		v := map[string]string{"notify_id": "local104-notify", "out_trade_no": o.TradeNo, "trade_no": "ALI104_" + o.TradeNo, "trade_status": "TRADE_SUCCESS", "total_amount": amount, "app_id": "local104-app", "seller_id": "local104-seller", "sign_type": "RSA2"}
		v["sign"] = sign([]byte(adapter.AlipayNotificationCanonical(v)))
		q := url.Values{}
		for k, x := range v {
			q.Set(k, x)
		}
		return []byte(q.Encode())
	}
	if e = flow.HandleNotification(t.Context(), 7, o.TradeNo, nil, callback("1.00")); !errors.Is(e, adapter.ErrAuthentication) {
		t.Fatal("callback M accepted", e)
	}
	if e = flow.HandleNotification(t.Context(), 7, o.TradeNo, nil, callback("1.06")); e != nil {
		t.Fatal("callback actual C rejected", e)
	}
	wrong.Store(false)
	if _, e = flow.Query(t.Context(), 10411, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	if _, e = flow.Refund(t.Context(), 10411, o.TradeNo, "actual-full104", 106); e == nil {
		t.Fatal("lost refund acceptance accepted")
	}
	if r, e := flow.QueryRefund(t.Context(), 10411, "actual-full104"); e != nil || r.Status != "succeeded" {
		t.Fatal("original actual RF recovery", r, e)
	}
	if creates.Load() != 1 || refunds.Load() != 1 || queries.Load() != 1 {
		t.Fatal("unexpected payment resend", creates.Load(), refunds.Load(), queries.Load())
	}
	current, e := s.OrderByTradeNo(t.Context(), o.TradeNo)
	if e != nil || current.Amount != 100 {
		t.Fatal("original merchant notification amount rewritten", current.Amount, e)
	}
}
