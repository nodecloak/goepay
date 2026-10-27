package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPreauthObservation68RealTLSAuthenticatedOriginalQuery(t *testing.T) {
	for _, kind := range []string{"partial", "explicit-zero-finish", "tampered-signature", "missing-rest", "wrong-auth"} {
		t.Run(kind, func(t *testing.T) {
			s, g, o, token, _ := observationFixture68(t)
			ctx := context.Background()
			before := observationFinancial68(t, s, o.TradeNo)
			app, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}
			platform, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}
			var calls atomic.Int32
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" {
					t.Error("not POST")
				}
				if e := r.ParseForm(); e != nil {
					t.Error(e)
					return
				}
				params := map[string]string{}
				for k, v := range r.PostForm {
					if len(v) != 1 {
						t.Error("duplicate request field")
					}
					params[k] = v[0]
				}
				signature, e := adapter.SignAlipayRSA2(params, app)
				if e != nil || signature != params["sign"] || params["app_id"] != "2021000000000068" || params["method"] != "alipay.fund.auth.operation.detail.query" {
					t.Error("wrong or unsigned method", e, params["method"])
				}
				var biz map[string]any
				if json.Unmarshal([]byte(params["biz_content"]), &biz) != nil || biz["out_order_no"] != o.TradeNo || biz["out_request_no"] != o.TradeNo || biz["out_request_no"] == strings.Repeat("4", 64) {
					t.Error("query did not use original freeze operation", biz)
				}
				out := map[string]any{"code": "10000", "out_order_no": o.TradeNo, "out_request_no": o.TradeNo, "auth_no": "original-auth68", "payer_user_id": "2088000000001068", "amount": "10.00", "rest_amount": "4.50", "operation_type": "FREEZE", "status": "SUCCESS", "order_status": "AUTHORIZED"}
				if kind == "missing-rest" {
					delete(out, "rest_amount")
				}
				if kind == "wrong-auth" {
					out["auth_no"] = "different-authorization"
				}
				if kind == "explicit-zero-finish" {
					out["rest_amount"] = "0.00"
					out["order_status"] = "FINISH"
				}
				node, _ := json.Marshal(out)
				digest := sha256.Sum256(node)
				sig, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
				if e != nil {
					t.Error(e)
					return
				}
				if kind == "tampered-signature" {
					out["rest_amount"] = "4.51"
					node, _ = json.Marshal(out)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"alipay_fund_auth_operation_detail_query_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(sig)})
			}))
			defer up.Close()
			real, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "2021000000000068", SellerID: "2088000000000068", PrivateKey: app, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, GatewayURL: up.URL, HTTPClient: up.Client()})
			if e != nil {
				t.Fatal(e)
			}
			g.Providers[7] = real
			q, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("4", 64)}, token, "merchant", observationCheck68, observationCheckTx68)
			if e != nil || calls.Load() != 1 {
				t.Fatal(q, e, calls.Load())
			}
			valid := kind == "partial" || kind == "explicit-zero-finish"
			if valid {
				expected := int64(450)
				if kind == "explicit-zero-finish" {
					expected = 0
				}
				if q.Status != "verified" || !q.SourceVerified || q.RestAmount == nil || *q.RestAmount != expected {
					t.Fatal("valid signed evidence rejected", q)
				}
			} else if q.Status != "unknown" || q.SourceVerified || q.RestAmount != nil {
				t.Fatal("untrusted response persisted evidence", q)
			}
			if observationFinancial68(t, s, o.TradeNo) != before {
				t.Fatal("read-only signed query changed financial state")
			}
		})
	}
}
