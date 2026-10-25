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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Losing the actual TLS or original request binding must remove eligibility,
// while leaving the existing uncertain scan outcome recoverable by query.
func TestScanPending78AuthenticatedOriginalResponseOnly(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	for _, kind := range []string{"alipay", "wxpay"} {
		for _, mode := range []string{"pending", "system", "plain-http", "insecure", "bad-sign", "other-code", "wrong-order", "wrong-account", "wrong-amount", "generic-error", "no-nonce", "echo-bearer"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				const apiKey = "synthetic-scan-pending78-api-key"
				calls := 0
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if kind == "alipay" {
						if r.ParseForm() != nil || r.Form.Get("method") != "alipay.trade.pay" {
							t.Error("wrong scan endpoint")
							return
						}
						code := "10003"
						if mode == "system" {
							code = "20000"
						}
						if mode == "other-code" {
							code = "40004"
						}
						node := map[string]string{"code": code, "sub_code": "ACQ.PAYMENT_AUTH_CODE_INVALID", "out_trade_no": "SP78_LOCAL", "seller_id": "2088000000000078", "app_id": "app78", "total_amount": "1.23"}
						if mode == "echo-bearer" {
							node["sub_code"] = "280000000000000078"
						}
						if mode == "wrong-order" {
							node["out_trade_no"] = "OTHER78"
						}
						if mode == "wrong-account" {
							node["seller_id"] = "other78"
						}
						if mode == "wrong-amount" {
							node["total_amount"] = "1.24"
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
						field := "alipay_trade_pay_response"
						if mode == "generic-error" {
							field = "error_response"
						}
						json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(sig)})
					} else {
						raw, _ := io.ReadAll(r.Body)
						p, e := decodeXML(raw)
						if e != nil || !VerifyWechatV2(p, apiKey, "MD5", p["sign"]) || r.URL.Path != "/pay/micropay" {
							t.Error("wrong micropay request")
							return
						}
						code := "USERPAYING"
						if mode == "system" {
							code = "SYSTEMERROR"
						}
						if mode == "other-code" {
							code = "AUTHCODEEXPIRE"
						}
						if mode == "echo-bearer" {
							code = "120000000000000078"
						}
						node := map[string]string{"return_code": "SUCCESS", "result_code": "FAIL", "err_code": code, "appid": "app78", "mch_id": "mch78", "nonce_str": "pending78", "out_trade_no": "SP78_LOCAL", "total_fee": "123"}
						if mode == "no-nonce" {
							delete(node, "nonce_str")
						}
						if mode == "wrong-order" {
							node["out_trade_no"] = "OTHER78"
						}
						if mode == "wrong-account" {
							node["mch_id"] = "other78"
						}
						if mode == "wrong-amount" {
							node["total_fee"] = "124"
						}
						node["sign"], _ = SignWechatV2(node, apiKey, "MD5")
						if mode == "bad-sign" {
							node["sign"] = "bad78"
						}
						w.Write(encodeXML(node))
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
				var base Provider
				var e error
				request := ScanRequest{TradeNo: "SP78_LOCAL", Subject: "Local scan", Amount: 123, ClientIP: "192.0.2.78", NotifyURL: "https://local.invalid/callback", AuthCode: "280000000000000078"}
				if kind == "alipay" {
					base, e = NewAlipay(AlipayConfig{AppID: "app78", SellerID: "2088000000000078", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: client, Methods: []string{"scan"}})
				} else {
					request.AuthCode = "120000000000000078"
					base, e = NewWechatV2(WechatV2Config{AppID: "app78", MerchantID: "mch78", APIKey: apiKey, BaseURL: up.URL, HTTPClient: client, Methods: []string{"scan"}})
				}
				if e != nil {
					t.Fatal(e)
				}
				_, e = base.(ScanProvider).Scan(context.Background(), request)
				receipt, valid := VerifiedScanPending(e)
				want := mode == "pending" || mode == "system" || kind == "alipay" && (mode == "no-nonce" || mode == "echo-bearer") || kind == "wxpay" && mode == "generic-error"
				if strings.Contains(fmt.Sprint(e), request.AuthCode) {
					t.Fatal("upstream business code echoed the bearer into the public scan error")
				}
				if valid != want || e == nil || calls != 1 {
					t.Fatalf("pending authority=%v want=%v error=%v calls=%d", valid, want, e, calls)
				}
				if !want {
					return
				}
				scope, _ := TransferAccountScope(base)
				if receipt.Provider != kind || receipt.AccountScope != scope || receipt.TradeNo != "SP78_LOCAL" || receipt.Amount != 123 || len(receipt.RequestSHA256) != 64 || len(receipt.ResponseSHA256) != 64 {
					t.Fatal("original pending provenance lost")
				}
				encoded, _ := json.Marshal(receipt)
				if strings.Contains(string(encoded), request.AuthCode) || strings.Contains(fmt.Sprint(e), request.AuthCode) {
					t.Fatal("bearer code exposed")
				}
				var rejected *ProviderError
				if !errors.As(e, &rejected) {
					t.Fatal("legacy provider error contract lost")
				}
				copy := *rejected
				copy.Code = "FORGED78"
				if _, ok := VerifiedScanPending(&copy); ok {
					t.Fatal("mutated code retained authority")
				}
				if _, ok := VerifiedScanPending(&UnknownOutcome{Operation: "scan", Cause: e}); ok {
					t.Fatal("uncertain wrapper grants automatic cancel authority")
				}
				if _, ok := VerifiedScanPending(errors.Join(e, context.Canceled)); ok {
					t.Fatal("cancelled call grants automatic cancel authority")
				}
				for _, wrapped := range []error{&UnknownOutcome{Operation: "scan", Cause: e}, errors.Join(e, context.Canceled), errors.Join(e, context.DeadlineExceeded)} {
					if _, ok := VerifiedScanPending(bindScanPending(wrapped, base, request)); ok {
						t.Fatal("binding stripped uncertainty and invented a new pending authority")
					}
				}
				data, _ := json.Marshal(rejected)
				var replay ProviderError
				json.Unmarshal(data, &replay)
				if _, ok := VerifiedScanPending(&replay); ok {
					t.Fatal("JSON forged pending authority")
				}
				if _, ok := VerifiedScanPending(fmt.Errorf("wrapped: %w", e)); !ok {
					t.Fatal("ordinary error wrapping lost provenance")
				}
			})
		}
	}
	if _, ok := VerifiedScanPending(&ProviderError{Code: "USERPAYING"}); ok {
		t.Fatal("bare text grants cancellation")
	}
}
