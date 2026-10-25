package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestScanCancellation78AlipayActualTLSOriginalRequestAuthenticatedFinalityAndNoRetry(t *testing.T) {
	merchant, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"close", "refund", "retry", "wrong-order", "wrong-provider", "bad-sign", "bad-action", "bad-no", "wrong-seller", "wrong-app", "plain-http"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if e := r.ParseForm(); e != nil {
					t.Error(e)
					return
				}
				params := map[string]string{}
				for key, values := range r.PostForm {
					if len(values) != 1 {
						t.Error("duplicate cancel field")
					}
					params[key] = values[0]
				}
				sig, e := SignAlipayRSA2(params, merchant)
				if e != nil || sig != params["sign"] || params["method"] != "alipay.trade.cancel" || params["app_id"] != "local-scan78" {
					t.Error("original cancellation request not signed")
				}
				var biz map[string]any
				if json.Unmarshal([]byte(params["biz_content"]), &biz) != nil || len(biz) != 1 || biz["out_trade_no"] != "SC78_LOCAL" {
					t.Error("cancel changed original request", biz)
				}
				node := map[string]any{"code": "10000", "out_trade_no": "SC78_LOCAL", "trade_no": "ALI78_LOCAL", "retry_flag": "N", "action": "close"}
				switch mode {
				case "refund":
					node["action"] = "refund"
				case "retry":
					node["retry_flag"] = "Y"
				case "wrong-order":
					node["out_trade_no"] = "OTHER78"
				case "wrong-provider":
					node["trade_no"] = "OTHER_PROVIDER78"
				case "bad-action":
					node["action"] = "success"
				case "bad-no":
					node["trade_no"] = "bad\r\nresponse"
				case "wrong-seller":
					node["seller_id"] = "2088000000000000"
				case "wrong-app":
					node["app_id"] = "different78"
				}
				raw, _ := json.Marshal(node)
				digest := sha256.Sum256(raw)
				signature, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
				if e != nil {
					t.Error(e)
					return
				}
				if mode == "bad-sign" {
					signature[0] ^= 1
				}
				json.NewEncoder(w).Encode(map[string]any{"alipay_trade_cancel_response": json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signature)})
			})
			up := httptest.NewTLSServer(handler)
			if mode == "plain-http" {
				up.Close()
				up = httptest.NewServer(handler)
			}
			defer up.Close()
			base, e := NewAlipay(AlipayConfig{EnableScanCancellation: true, AppID: "local-scan78", SellerID: "2088000000000078", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client(), Methods: []string{"scan"}})
			if e != nil {
				t.Fatal(e)
			}
			canceller, ok := any(base).(ScanCancellationProvider)
			if !ok {
				t.Fatal("Alipay has no typed scan cancellation")
			}
			request := ScanCancellationRequest{TradeNo: "SC78_LOCAL", ProviderTradeNo: "ALI78_LOCAL", Amount: 123}
			if mode == "bad-no" {
				request.ProviderTradeNo = ""
			}
			result, e := canceller.CancelScan(context.Background(), request)
			if mode == "close" || mode == "refund" {
				if !ScanCancellationMatchesRequest(result, request) {
					t.Fatal("original request digest binding lost")
				}
				different := request
				different.ProviderTradeNo = ""
				if ScanCancellationMatchesRequest(result, different) {
					t.Fatal("response institution number substituted for original request context")
				}
				proof, valid := VerifiedScanCancellation(result)
				scope, _ := TransferAccountScope(base)
				if e != nil || !valid || result.State != "cancelled" || result.Action != mode || proof.Product != "alipay.trade.cancel" || proof.AccountScope != scope || proof.Amount != 123 || len(proof.RequestSHA256) != 64 || len(proof.ResponseSHA256) != 64 {
					t.Fatal("valid cancellation proof missing", e)
				}
				changed := result
				changed.Amount++
				if _, valid := VerifiedScanCancellation(changed); valid {
					t.Fatal("mutated original amount retained proof")
				}
				raw, _ := json.Marshal(result)
				var copy ScanCancellationResult
				if json.Unmarshal(raw, &copy) != nil {
					t.Fatal("result JSON invalid")
				}
				if _, valid := VerifiedScanCancellation(copy); valid {
					t.Fatal("plain JSON forged authenticated cancellation")
				}
			} else {
				if e == nil {
					t.Fatal("untrusted or unfinished cancellation accepted")
				}
				if _, valid := VerifiedScanCancellation(result); valid {
					t.Fatal("invalid cancellation gained final proof")
				}
			}
			want := int32(1)
			if mode == "plain-http" {
				want = 0
			}
			if calls.Load() != want {
				t.Fatal("invalid request sent or cancellation retried", calls.Load(), want)
			}
		})
	}
}
