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
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This opt-in actually runs the distributable JS client against the Go handler
// and a fresh PostgreSQL schema. Both providers are pinned local TLS fixtures.
func TestMiniClient81NodeHTTPInterop(t *testing.T) {
	if os.Getenv("GOEPAY_MINI_CLIENT_INTEROP") != "1" {
		t.Skip("opt-in Node mini-client interoperability is executed separately")
	}
	node, e := exec.LookPath("node")
	if e != nil {
		t.Fatal("explicit Node interoperability requires installed Node")
	}
	runner, e := filepath.Abs(filepath.Join("..", "..", "clients", "mini", "tests", "http-contract-runner.cjs"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(runner); e != nil {
		t.Fatal("mini-client HTTP bridge is not implemented")
	}
	for _, kind := range []string{"wxpay", "alipay"} {
		t.Run(kind, func(t *testing.T) {
			s, pool, merchantKey := fixture(t)
			s.Sandbox = false
			ctx := context.Background()
			var exchanges, creates atomic.Int64
			platform, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}
			key := "0123456789abcdef0123456789abcdef"
			const aliTrade = "2026102422000000000000000001"
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "wxpay" {
					switch r.URL.Path {
					case "/sns/jscode2session":
						exchanges.Add(1)
						if r.URL.Query().Get("js_code") != "mini81-synthetic-code" || r.URL.Query().Get("appid") != "wxapp" {
							t.Error("client did not supply original application code")
						}
						json.NewEncoder(w).Encode(map[string]string{"openid": "mini81-buyer", "session_key": "mini81-discard-key"})
					case "/pay/unifiedorder":
						creates.Add(1)
						raw := new(bytes.Buffer)
						raw.ReadFrom(r.Body)
						if !strings.Contains(raw.String(), "<openid>mini81-buyer</openid>") || !strings.Contains(raw.String(), "<total_fee>125</total_fee>") {
							t.Error("unverified payer or client display money used")
						}
						out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "wxapp", "mch_id": "mch", "prepay_id": "mini81-original-prepay", "trade_type": "JSAPI"}
						out["sign"], _ = adapter.SignWechatV2(out, key, "MD5")
						w.Write(providerXML(out))
					default:
						t.Error("unexpected local Wechat operation", r.URL.Path)
						w.WriteHeader(500)
					}
					return
				}
				r.ParseForm()
				var field string
				var out map[string]any
				switch r.Form.Get("method") {
				case "alipay.system.oauth.token":
					exchanges.Add(1)
					field = "alipay_system_oauth_token_response"
					out = map[string]any{"user_id": "2088000000000000", "access_token": "mini81-discard-token", "expires_in": "3600"}
				case "alipay.trade.create":
					creates.Add(1)
					var body map[string]any
					json.Unmarshal([]byte(r.Form.Get("biz_content")), &body)
					if body["buyer_id"] != "2088000000000000" || body["total_amount"] != "1.25" {
						t.Error("unverified payer or display money used")
					}
					field = "alipay_trade_create_response"
					out = map[string]any{"code": "10000", "out_trade_no": body["out_trade_no"], "trade_no": aliTrade}
				default:
					t.Error("unexpected local Alipay operation", r.Form.Get("method"))
					w.WriteHeader(500)
					return
				}
				raw, _ := json.Marshal(out)
				digest := sha256.Sum256(raw)
				signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
				json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signature)})
			}))
			defer up.Close()
			var p adapter.Provider
			var oauth adapter.MiniOAuthProvider
			appID, method := "wxapp", "jsapi"
			if kind == "wxpay" {
				client := &http.Client{Transport: oauthWechatTLSTransport{up, t}}
				p, e = adapter.NewWechatV2(adapter.WechatV2Config{AppID: appID, MerchantID: "mch", APIKey: key, Methods: []string{"jsapi"}, MiniProgram: &adapter.MiniProgramConfig{Path: "pages/pay/pay"}, HTTPClient: client})
				if e != nil {
					t.Fatal(e)
				}
				oauth, e = adapter.NewWechatMiniOAuth(adapter.WechatOAuthConfig{AppID: appID, AppSecret: "mini81-synthetic-secret", HTTPClient: client})
				if e != nil {
					t.Fatal(e)
				}
			} else {
				appID, method = "app", "mini"
				ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: appID, PrivateKey: merchantKey, PublicKey: &platform.PublicKey, Methods: []string{"mini"}, MiniProgram: &adapter.MiniProgramConfig{Path: "pages/pay/pay"}, HTTPClient: &http.Client{Transport: oauthContractTLSTransport{up, t}}})
				if e != nil {
					t.Fatal(e)
				}
				p, oauth = ali, ali
			}
			s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
			s.MiniOAuthProviders = map[int64]adapter.MiniOAuthProvider{7: oauth}
			s.MerchantChannels = map[int64][]int64{1: {7}}
			s.CheckoutKey = bytes.Repeat([]byte{8}, 32)
			o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "mini81-" + kind, Type: kind, Name: "item", Amount: 125})
			if e != nil {
				t.Fatal(e)
			}
			if e = s.Store.SetOrderClientIP(ctx, o.ID, "192.0.2.5"); e != nil {
				t.Fatal(e)
			}
			binding := payment.MiniLaunchBinding{TradeNo: o.TradeNo, MerchantID: 1, ChannelID: 7, Provider: kind, AppID: appID, Method: method, Purpose: "mini-payment", Path: "pages/pay/pay"}
			if e = s.Store.PrepareMiniPayment(ctx, binding); e != nil {
				t.Fatal(e)
			}
			state, e := s.Store.IssueMiniState(ctx, binding)
			if e != nil {
				t.Fatal(e)
			}
			local := httptest.NewServer(s.Handler())
			defer local.Close()
			input, _ := json.Marshal(map[string]string{"provider": kind, "appId": appID, "completionURL": fmt.Sprintf("https://pay.example.invalid/oauth/mini/complete/%s/7/%s/?state=%s", kind, o.TradeNo, state), "localServer": local.URL, "money": "99.99"})
			runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(runCtx, node, runner)
			cmd.Stdin = bytes.NewReader(input)
			output, e := cmd.CombinedOutput()
			if e != nil {
				t.Fatalf("Node client failed: %v %s", e, output)
			}
			var result struct {
				Phase                   string `json:"phase"`
				Posts, Logins, Payments int
			}
			if e = json.Unmarshal(output, &result); e != nil || result.Phase != "verification_required" || result.Posts != 1 || result.Logins != 1 || result.Payments != 1 || exchanges.Load() != 1 || creates.Load() != 1 {
				t.Fatalf("unexpected mini client counts: %s %v", output, e)
			}
			bal, e := s.Store.Balance(ctx, 1)
			if e != nil || bal.Available != 0 {
				t.Fatal("SDK callback credited ledger", bal, e)
			}
			o, e = s.Store.OrderByTradeNo(ctx, o.TradeNo)
			if e != nil || o.Status == "paid" {
				t.Fatal("SDK callback marked paid", e)
			}
			var attempts, receipts int
			if e = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM provider_attempts WHERE order_id=$1),(SELECT count(*) FROM verified_payment_receipts WHERE order_id=$1)", o.ID).Scan(&attempts, &receipts); e != nil || attempts != 1 || receipts != 0 {
				t.Fatal(attempts, receipts, e)
			}
		})
	}
}
