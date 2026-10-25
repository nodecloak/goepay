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
	"testing"
)

func TestScanObservation78RequiresActualTLSAndOriginalAuthenticatedRead(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	for _, kind := range []string{"alipay", "wxpay"} {
		for _, mode := range []string{"pending", "success", "closed", "plain-http", "insecure", "bad-sign", "wrong-order", "wrong-account", "wrong-provider", "unknown-state", "generic-error", "no-nonce"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				const apiKey = "synthetic-scan-observation-api-key78"
				calls := 0
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if kind == "alipay" {
						if r.ParseForm() != nil || r.PostForm.Get("method") != "alipay.trade.query" {
							t.Error("observation used a financial endpoint")
							return
						}
						state := "WAIT_BUYER_PAY"
						if mode == "success" {
							state = "TRADE_SUCCESS"
						}
						if mode == "closed" {
							state = "TRADE_CLOSED"
						}
						if mode == "unknown-state" {
							state = "UNKNOWN78"
						}
						node := map[string]string{"code": "10000", "out_trade_no": "OBS78_LOCAL", "trade_no": "UP78_LOCAL", "trade_status": state, "total_amount": "1.23", "buyer_user_id": "local-buyer78", "app_id": "app78", "seller_id": "seller78"}
						if mode == "wrong-order" {
							node["out_trade_no"] = "OTHER78"
						}
						if mode == "wrong-account" {
							node["seller_id"] = "other78"
						}
						if mode == "wrong-provider" {
							node["trade_no"] = "OTHER_UP78"
						}
						raw, _ := json.Marshal(node)
						digest := sha256.Sum256(raw)
						sig, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
						if e != nil {
							t.Error(e)
							return
						}
						if mode == "bad-sign" {
							sig[0] ^= 1
						}
						field := "alipay_trade_query_response"
						if mode == "generic-error" {
							field = "error_response"
						}
						json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(sig)})
					} else {
						if r.URL.Path != "/pay/orderquery" {
							t.Error("observation used a financial endpoint")
							return
						}
						state := "USERPAYING"
						if mode == "success" {
							state = "SUCCESS"
						}
						if mode == "closed" {
							state = "CLOSED"
						}
						if mode == "unknown-state" {
							state = "UNKNOWN78"
						}
						out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app78", "mch_id": "mch78", "out_trade_no": "OBS78_LOCAL", "transaction_id": "UP78_LOCAL", "total_fee": "123", "openid": "local-buyer78", "nonce_str": "observation78", "trade_state": state}
						if mode == "wrong-order" {
							out["out_trade_no"] = "OTHER78"
						}
						if mode == "wrong-account" {
							out["mch_id"] = "other78"
						}
						if mode == "wrong-provider" {
							out["transaction_id"] = "OTHER_UP78"
						}
						if mode == "no-nonce" {
							delete(out, "nonce_str")
						}
						out["sign"], _ = SignWechatV2(out, apiKey, "MD5")
						if mode == "bad-sign" {
							out["sign"] = "bad78"
						}
						w.Write(encodeXML(out))
					}
				})
				up := httptest.NewTLSServer(handler)
				if mode == "plain-http" {
					up.Close()
					up = httptest.NewServer(handler)
				}
				defer up.Close()
				client := up.Client()
				if mode == "insecure" {
					tr := client.Transport.(*http.Transport).Clone()
					tr.TLSClientConfig = tr.TLSClientConfig.Clone()
					tr.TLSClientConfig.InsecureSkipVerify = true
					defer tr.CloseIdleConnections()
					client = &http.Client{Transport: tr}
				}
				var p Provider
				var e error
				if kind == "alipay" {
					p, e = NewAlipay(AlipayConfig{AppID: "app78", SellerID: "seller78", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: client, Methods: []string{"scan"}})
				} else {
					p, e = NewWechatV2(WechatV2Config{AppID: "app78", MerchantID: "mch78", APIKey: apiKey, BaseURL: up.URL, HTTPClient: client, Methods: []string{"scan"}})
				}
				if e != nil {
					t.Fatal(e)
				}
				observer, ok := p.(ScanObservationProvider)
				if !ok {
					t.Fatal("provider lacks a typed authenticated scan read")
				}
				result, e := observer.ObserveScan(context.Background(), QueryRequest{TradeNo: "OBS78_LOCAL", ProviderTradeNo: "UP78_LOCAL"})
				receipt, valid := VerifiedScanObservation(result)
				want := mode == "pending" || mode == "success" || mode == "closed" || kind == "alipay" && mode == "no-nonce" || kind == "wxpay" && mode == "generic-error"
				if valid != want || want && e != nil || !want && e == nil {
					t.Fatalf("observation authority=%v want=%v error=%v", valid, want, e)
				}
				if mode == "plain-http" {
					if calls != 0 {
						t.Fatal("plaintext observation performed IO")
					}
					return
				}
				if calls != 1 {
					t.Fatal("read retried or skipped", calls)
				}
				if !want {
					return
				}
				scope, _ := TransferAccountScope(p)
				if receipt.TradeNo != "OBS78_LOCAL" || receipt.ProviderTradeNo != "UP78_LOCAL" || receipt.Amount != 123 || receipt.Currency != "CNY" || receipt.AccountScope != scope || len(receipt.ResponseSHA256) != 64 || len(receipt.RequestSHA256) != 64 {
					t.Fatal("original observation binding lost")
				}
				changed := result
				changed.BuyerID = "changed78"
				if _, ok := VerifiedScanObservation(changed); ok {
					t.Fatal("modified buyer retained provenance")
				}
				changed = result
				changed.Amount++
				if _, ok := VerifiedScanObservation(changed); ok {
					t.Fatal("modified amount retained provenance")
				}
				raw, _ := json.Marshal(result)
				var replay QueryResult
				json.Unmarshal(raw, &replay)
				if _, ok := VerifiedScanObservation(replay); ok {
					t.Fatal("JSON forged an authenticated observation")
				}
				ordinary, e := p.Query(context.Background(), QueryRequest{TradeNo: "OBS78_LOCAL", ProviderTradeNo: "UP78_LOCAL"})
				if e != nil {
					t.Fatal("ordinary signed query regressed", e)
				}
				if _, ok := VerifiedScanObservation(ordinary); ok {
					t.Fatal("ordinary query acquired bounded polling provenance")
				}
				if calls != 2 {
					t.Fatal("ordinary query retried", calls)
				}
			})
		}
	}
	if _, ok := VerifiedScanObservation(QueryResult{TradeNo: "OBS78_LOCAL", Amount: 123, Currency: "CNY", State: "USERPAYING"}); ok {
		t.Fatal("caller-built result grants polling proof")
	}
}

func TestScanObservation78InvalidAndDisabledReadPerformsNoIO(t *testing.T) {
	calls := 0
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "unexpected78", 500) }))
	defer up.Close()
	key := testKey(t)
	for _, kind := range []string{"alipay", "wxpay"} {
		for _, mode := range []string{"disabled", "empty-number", "long-number", "bad-provider-number", "cancelled"} {
			methods := []string{"scan"}
			if mode == "disabled" {
				methods = []string{"native"}
			}
			var p Provider
			var e error
			if kind == "alipay" {
				p, e = NewAlipay(AlipayConfig{AppID: "app78", SellerID: "seller78", PrivateKey: key, PublicKey: &key.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client(), Methods: methods})
			} else {
				p, e = NewWechatV2(WechatV2Config{AppID: "app78", MerchantID: "mch78", APIKey: "synthetic-observation-api-key78", BaseURL: up.URL, HTTPClient: up.Client(), Methods: methods})
			}
			if e != nil {
				t.Fatal(e)
			}
			request := QueryRequest{TradeNo: "OBS78_LOCAL"}
			switch mode {
			case "empty-number":
				request.TradeNo = ""
			case "long-number":
				request.TradeNo = string(make([]byte, 65))
			case "bad-provider-number":
				request.ProviderTradeNo = "bad\r\n78"
			}
			ctx, cancel := context.WithCancel(context.Background())
			if mode == "cancelled" {
				cancel()
			}
			_, e = p.(ScanObservationProvider).ObserveScan(ctx, request)
			cancel()
			if e == nil || calls != 0 {
				t.Fatal("invalid observation reached upstream", kind, mode, e, calls)
			}
		}
	}
}
