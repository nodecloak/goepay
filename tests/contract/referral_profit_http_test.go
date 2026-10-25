package contract_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/enrollment"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type profitHTTPProvider71 struct {
	*adapter.WechatV3
	cost adapter.PaymentCostEstimate
}

func (p profitHTTPProvider71) PaymentCostEstimate() (adapter.PaymentCostEstimate, bool) {
	return p.cost, true
}

func TestReferralProfit71PublicCreateTrustedCostAndSignedTLSCallback(t *testing.T) {
	for _, mode := range []string{"immediate", "daily"} {
		t.Run(mode, func(t *testing.T) {
			s, pool, merchantKey := fixture(t)
			ctx := context.Background()
			issued, e := s.Store.RotateReferralToken(ctx, 1, 0, payment.TransferBatchActor{Role: "merchant", ID: 1, Name: "synthetic-promoter"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"registration_open","value":"true"}')`); e != nil {
				t.Fatal(e)
			}
			var code string
			en, e := enrollment.New(pool, s.Store, make([]byte, 32), enrollment.SenderFunc(func(_ context.Context, _, _, v string) error { code = v; return nil }))
			if e != nil {
				t.Fatal(e)
			}
			challenge, e := en.Issue(ctx, enrollment.VerificationRequest{Purpose: "register", Email: "profit71@example.test", Source: "192.0.2.71"})
			if e != nil {
				t.Fatal(e)
			}
			registered, e := en.Register(ctx, enrollment.RegisterInput{ChallengeID: challenge.ID, Code: code, Email: "profit71@example.test", Source: "192.0.2.71", Username: "profit71", Name: "synthetic profit merchant", Password: "synthetic-profit-password-71", PromotionToken: issued.Token})
			if e != nil {
				t.Fatal(e)
			}
			var gid int64
			if e = pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"profit71 fee","enabled":true,"rate_bps":"1000"}') RETURNING id`).Scan(&gid); e != nil {
				t.Fatal(e)
			}
			if _, e = pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_build_object('group_id',$2::bigint::text) WHERE id=$1`, registered.MerchantID, gid); e != nil {
				t.Fatal(e)
			}
			if _, e = pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}'),('settings','{"key":"refund_fee_payer","value":"platform"}')`); e != nil {
				t.Fatal(e)
			}
			yes, bps := true, int64(5000)
			if _, e = s.Store.SetReferralPolicy(ctx, "global", 0, 0, payment.ReferralPolicyInput{Enabled: &yes, RateBPS: &bps, Mode: mode, Basis: "profit", RefundPolicy: "any_successful_refund_voids_reward"}, payment.TransferBatchActor{Role: "admin", Class: "super", ID: 1, Name: "synthetic-policy"}); e != nil {
				t.Fatal(e)
			}
			platform, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}
			var calls atomic.Int32
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v3/pay/transactions/native" {
					t.Error("unexpected provider path", r.URL.Path)
					w.WriteHeader(500)
					return
				}
				calls.Add(1)
				var request map[string]any
				if json.NewDecoder(r.Body).Decode(&request) != nil || request["appid"] != "profit71-app" || request["mchid"] != "profit71-mch" {
					t.Error("physical request identity", request)
				}
				if _, ok := request["costrate"]; ok {
					t.Error("configured estimate leaked into provider request")
				}
				raw := []byte(`{"code_url":"weixin://wxpay/bizpayurl?pr=synthetic-profit71"}`)
				for k, v := range v3ContractHeaders(t, raw, platform) {
					w.Header()[k] = v
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write(raw)
			}))
			defer up.Close()
			apiKey := []byte("0123456789abcdef0123456789abcdef")
			p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "profit71-app", MerchantID: "profit71-mch", MerchantSerial: "SYNTHETIC71", PrivateKey: merchantKey, APIv3Key: apiKey, VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_HTTP": &platform.PublicKey}, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: v3ContractTLS{up}}})
			if e != nil {
				t.Fatal(e)
			}
			cost, e := adapter.NewPaymentCostEstimate(p, "5")
			if e != nil {
				t.Fatal(e)
			}
			s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: profitHTTPProvider71{p, cost}}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
			s.Sandbox = false
			s.CheckoutKey = bytes.Repeat([]byte{71}, 32)
			s.MerchantChannels = map[int64][]int64{registered.MerchantID: {7}}
			merchant, e := s.Store.Merchant(ctx, registered.MerchantID)
			if e != nil {
				t.Fatal(e)
			}
			params := compat.Params{"pid": fmt.Sprint(registered.MerchantID), "out_trade_no": "profit71-public", "type": "wxpay", "method": "native", "name": "synthetic71", "money": "1.00", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "costrate": "0", "estimated_cost": "0", "profit": "1000000"}
			path, values, success := "/mapi.php", signed(params, merchant.APIKey), float64(1)
			if mode == "daily" {
				der, err := x509.MarshalPKIXPublicKey(&merchantKey.PublicKey)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = pool.Exec(ctx, `UPDATE merchants SET signature_algorithm='RSA',rsa_public_key=$2 WHERE id=$1`, registered.MerchantID, base64.StdEncoding.EncodeToString(der)); err != nil {
					t.Fatal(err)
				}
				params["sign_type"], params["timestamp"] = "RSA", strconv.FormatInt(time.Now().Unix(), 10)
				params["sign"], err = compat.SignRSA(params, merchantKey)
				if err != nil {
					t.Fatal(err)
				}
				path, values, success = "/api/pay/create", url.Values{}, 0
				for k, v := range params {
					values.Set(k, v)
				}
			}
			status, result := call(t, s.Handler(), "POST", path, values)
			if status != 200 || result["code"] != success || calls.Load() != 1 {
				t.Fatal("public profit creation", status, result, calls.Load())
			}
			o, e := s.Store.Order(ctx, registered.MerchantID, "profit71-public")
			if e != nil {
				t.Fatal(e)
			}
			var units, reward int64
			if e = pool.QueryRow(ctx, "SELECT rate_units,reward_amount FROM referral_profit_costs WHERE order_id=$1", o.ID).Scan(&units, &reward); e != nil || units != 5000000 || reward != 3 {
				t.Fatal("merchant overrode trusted cost", units, reward, e)
			}
			callback := func(amount int64, event string) int {
				transaction, _ := json.Marshal(map[string]any{"appid": "profit71-app", "mchid": "profit71-mch", "out_trade_no": o.TradeNo, "transaction_id": "WX_PROFIT71_" + o.TradeNo, "trade_state": "SUCCESS", "amount": map[string]any{"total": amount, "currency": "CNY"}, "payer": map[string]string{"openid": "synthetic-profit71-buyer"}})
				block, _ := aes.NewCipher(apiKey)
				gcm, _ := cipher.NewGCM(block)
				nonce := []byte("0123456789ab")
				encrypted := gcm.Seal(nil, nonce, transaction, []byte("transaction"))
				body, _ := json.Marshal(map[string]any{"id": event, "event_type": "TRANSACTION.SUCCESS", "resource_type": "encrypt-resource", "resource": map[string]string{"original_type": "transaction", "algorithm": "AEAD_AES_256_GCM", "nonce": string(nonce), "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(encrypted)}})
				r := httptest.NewRequest("POST", "/callbacks/providers/7/"+o.TradeNo, bytes.NewReader(body))
				r.Header = v3ContractHeaders(t, body, platform)
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				if w.Code != 204 && amount == 100 {
					t.Logf("synthetic callback status=%d body=%s local_verify=%v", w.Code, w.Body.String(), s.ProviderFlow.HandleNotification(ctx, 7, o.TradeNo, r.Header, body))
				}
				return w.Code
			}
			if callback(101, "BAD_AMOUNT71") == 204 {
				t.Fatal("mismatched signed amount accepted")
			}
			if callback(100, "PROFIT71_EVENT") != 204 || callback(100, "PROFIT71_EVENT") != 204 {
				t.Fatal("authenticated callback/replay failed")
			}
			balance, e := s.Store.Balance(ctx, 1)
			want := int64(3)
			if mode == "daily" {
				want = 0
			}
			if e != nil || balance.Available != want {
				t.Fatal(balance, e)
			}
			report, e := s.Store.CheckLedger(ctx)
			if e != nil || report.InvalidReferralTerms+report.InvalidReferralRewards+report.InvalidReferralClawbacks+report.InvalidAccounts+report.InvalidJournals != 0 {
				t.Fatal(report, e)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "cost") || strings.Contains(string(raw), cost.Revision) {
				t.Fatal("private estimate exposed", string(raw))
			}
		})
	}
}
