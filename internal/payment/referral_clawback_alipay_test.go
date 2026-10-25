package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func TestReferralClawbackActualAlipayTLSOriginalRefundAndSignatureAreRequired(t *testing.T) {
	s := referralClawbackSetup(t, "platform")
	ctx := context.Background()
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 902, OutTradeNo: "ali-referral-payment", Name: "local original Alipay item", Type: "alipay", Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var phase, sends, queries atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		params := map[string]string{}
		for k, vs := range r.Form {
			if len(vs) != 1 {
				t.Error("duplicate Alipay parameter", k)
				w.WriteHeader(400)
				return
			}
			params[k] = vs[0]
		}
		digest := sha256.Sum256([]byte(adapter.AlipayRequestCanonical(params)))
		signature, err := base64.StdEncoding.DecodeString(params["sign"])
		if err != nil || rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, digest[:], signature) != nil {
			t.Error("request signature did not authenticate local merchant")
			w.WriteHeader(400)
			return
		}
		var biz map[string]string
		if json.Unmarshal([]byte(params["biz_content"]), &biz) != nil || biz["out_trade_no"] != o.TradeNo {
			t.Error("request lost original Alipay payment", biz)
			w.WriteHeader(400)
			return
		}
		out := map[string]any{"code": "10000", "out_trade_no": o.TradeNo, "trade_no": "ALI_" + o.TradeNo}
		switch params["method"] {
		case "alipay.trade.query":
			out["total_amount"] = "1.00"
			out["trade_status"] = "TRADE_SUCCESS"
			out["buyer_user_id"] = "2088000000001234"
		case "alipay.trade.refund":
			sends.Add(1)
			if !strings.HasPrefix(biz["out_request_no"], "RF") || biz["refund_amount"] != "0.98" {
				t.Error("refund lost original RF or amount", biz)
				w.WriteHeader(400)
				return
			}
			w.WriteHeader(503)
			return
		case "alipay.trade.fastpay.refund.query":
			queries.Add(1)
			var expected string
			if err := s.pool.QueryRow(ctx, "SELECT 'RF'||id::text FROM refunds WHERE merchant_id=902 AND refund_no='ali-refund'").Scan(&expected); err != nil || biz["out_request_no"] != expected {
				t.Error("query changed original durable RF", biz, err)
				w.WriteHeader(400)
				return
			}
			out["out_request_no"] = expected
			out["refund_amount"] = "0.98"
			out["refund_status"] = "REFUND_SUCCESS"
			if phase.Load() == 1 {
				out["refund_amount"] = "0.99"
			}
			if phase.Load() == 2 {
				out["out_request_no"] = "RF_FOREIGN"
			}
		default:
			t.Error("unexpected synthetic Alipay method", params["method"])
			w.WriteHeader(400)
			return
		}
		raw, _ := json.Marshal(out)
		hash := sha256.Sum256(raw)
		signed, err := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, hash[:])
		if err != nil {
			t.Error(err)
			return
		}
		if phase.Load() == 3 {
			signed[0] ^= 1
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(params["method"], ".", "_") + "_response": json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signed)})
	}))
	defer up.Close()
	p, err := adapter.NewAlipay(adapter.AlipayConfig{AppID: "local-referral-ali", SellerID: "2088000000000009", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client(), Methods: []string{"app"}})
	if err != nil {
		t.Fatal(err)
	}
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://local.example.invalid"}
	if _, err = flow.Submit(ctx, o.TradeNo, 7, "app", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = flow.Query(ctx, 902, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	if _, err = flow.Refund(ctx, 902, o.TradeNo, "ali-refund", 98); err == nil {
		t.Fatal("lost Alipay acceptance proved refund")
	}
	referralClawbackBalances(t, s, 0, 8, 2, 0)
	for _, bad := range []int32{1, 2, 3} {
		phase.Store(bad)
		if _, err = flow.QueryRefund(ctx, 902, "ali-refund"); !errors.Is(err, adapter.ErrAuthentication) {
			t.Fatalf("untrusted signed Alipay result accepted phase%d: %v", bad, err)
		}
		referralClawbackBalances(t, s, 0, 8, 2, 0)
	}
	phase.Store(4)
	r, err := flow.QueryRefund(ctx, 902, "ali-refund")
	if err != nil || r.Status != "succeeded" {
		t.Fatal(r, err)
	}
	referralClawbackBalances(t, s, 2, 0, 0, 0)
	if _, err = flow.Refund(ctx, 902, o.TradeNo, "ali-refund", 98); err != nil {
		t.Fatal(err)
	}
	if _, err = flow.QueryRefund(ctx, 902, "ali-refund"); err != nil {
		t.Fatal(err)
	}
	if sends.Load() != 1 || queries.Load() != 4 {
		t.Fatal("Alipay recovery resent original financial request", sends.Load(), queries.Load())
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidReferralClawbacks != 0 || report.InvalidReferralRewards != 0 || report.InvalidProviderRefundResults != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatal(report, err)
	}
}
