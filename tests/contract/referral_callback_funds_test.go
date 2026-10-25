package contract_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/enrollment"
	"goepay/internal/payment"
)

func TestReferralAuthenticatedCallbackAndRefundFundsSnapshot(t *testing.T) {
	s, pool, merchantKey := fixture(t)
	ctx := context.Background()
	issued, err := s.Store.RotateReferralToken(ctx, 1, 0, payment.TransferBatchActor{Role: "merchant", ID: 1, Name: "local-promoter"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"registration_open","value":"true"}')`); err != nil {
		t.Fatal(err)
	}
	var code string
	registration, err := enrollment.New(pool, s.Store, make([]byte, 32), enrollment.SenderFunc(func(_ context.Context, _, _, value string) error { code = value; return nil }))
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := registration.Issue(ctx, enrollment.VerificationRequest{Purpose: "register", Email: "referral-callback@example.test", Source: "192.0.2.20"})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := registration.Register(ctx, enrollment.RegisterInput{ChallengeID: challenge.ID, Code: code, Email: "referral-callback@example.test", Source: "192.0.2.20", Username: "referral_callback_user", Name: "Local invited store", Password: "local-referral-password-123", PromotionToken: issued.Token})
	if err != nil {
		t.Fatal(err)
	}
	var gid int64
	if err = pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"local callback fee","enabled":true,"rate_bps":"1000"}') RETURNING id`).Scan(&gid); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_build_object('group_id',$2::bigint::text) WHERE id=$1`, registered.MerchantID, gid); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}'),('settings','{"key":"refund_fee_payer","value":"platform"}')`); err != nil {
		t.Fatal(err)
	}
	enabled, rate := true, int64(5000)
	if _, err = s.Store.SetReferralPolicy(ctx, "global", 0, 0, payment.ReferralPolicyInput{Enabled: &enabled, RateBPS: &rate, Mode: "immediate", Basis: "fee", RefundPolicy: "any_successful_refund_voids_reward"}, payment.TransferBatchActor{Role: "admin", Class: "super", ID: 1, Name: "local-admin"}); err != nil {
		t.Fatal(err)
	}
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	apiKey := []byte("0123456789abcdef0123456789abcdef")
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/pay/transactions/native" {
			t.Error("unexpected local provider operation", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		raw := []byte(`{"code_url":"weixin://wxpay/bizpayurl?pr=local-referral"}`)
		for k, v := range v3ContractHeaders(t, raw, platform) {
			w.Header()[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}))
	defer up.Close()
	p, err := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchantKey, APIv3Key: apiKey, VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_HTTP": &platform.PublicKey}, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: v3ContractTLS{up}}})
	if err != nil {
		t.Fatal(err)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	o, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: registered.MerchantID, OutTradeNo: "callback-award", Name: "local ordinary item", Type: "wxpay", Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProviderFlow.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.MarkPaid(ctx, o.TradeNo, "sandbox-is-not-provider-proof"); !errors.Is(err, payment.ErrState) {
		t.Fatal("sandbox bypassed live receipt", err)
	}
	transaction, _ := json.Marshal(map[string]any{"appid": "local-app", "mchid": "local-mch", "out_trade_no": o.TradeNo, "transaction_id": "WX_REFERRAL_" + o.TradeNo, "trade_state": "SUCCESS", "amount": map[string]any{"total": 100, "currency": "CNY"}, "payer": map[string]string{"openid": "local-referral-buyer"}})
	block, _ := aes.NewCipher(apiKey)
	gcm, _ := cipher.NewGCM(block)
	nonce := []byte("0123456789ab")
	encrypted := gcm.Seal(nil, nonce, transaction, []byte("transaction"))
	body, _ := json.Marshal(map[string]any{"id": "REFERRAL_EVENT", "event_type": "TRANSACTION.SUCCESS", "resource_type": "encrypt-resource", "resource": map[string]string{"original_type": "transaction", "algorithm": "AEAD_AES_256_GCM", "nonce": string(nonce), "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(encrypted)}})
	headers := v3ContractHeaders(t, body, platform)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "/callbacks/providers/7/"+o.TradeNo, bytes.NewReader(body))
			r.Header = headers.Clone()
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != 204 {
				t.Errorf("authenticated callback status=%d body=%s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	balance, err := s.Store.Balance(ctx, 1)
	if err != nil || balance.Available != 5 {
		t.Fatal("callback reward duplicated or unfunded", balance, err)
	}
	target := "/console/v1/admin/orders/" + o.TradeNo + "/refund"
	actionBody := json.RawMessage(`{"amount":"0.98","external_no":"callback-refund"}`)
	before, summary, err := s.ResolveFundsAction(ctx, pool, target, actionBody)
	if err != nil || summary["referral_recovery_amount"] != "0.05" || summary["referral_fee_contribution"] != "0.03" || summary["referral_promoter_id"] != "1" {
		t.Fatal(summary, err)
	}
	r, err := s.Store.ReserveRefund(ctx, registered.MerchantID, o.OutTradeNo, "callback-refund", 98)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.ResolveRefund(ctx, r.ID, "unknown"); err != nil {
		t.Fatal(err)
	}
	during, summary, err := s.ResolveFundsAction(ctx, pool, target, actionBody)
	if err != nil || bytes.Equal(before.Digest(), during.Digest()) || summary["referral_recovery_state"] != "unknown" {
		t.Fatal("funds authorization lost original escrow state", summary, err)
	}
	if err = s.Store.ResolveRefund(ctx, r.ID, "failed"); err != nil {
		t.Fatal(err)
	}
	after, _, err := s.ResolveFundsAction(ctx, pool, target, actionBody)
	if err != nil || bytes.Equal(during.Digest(), after.Digest()) {
		t.Fatal("restored proof did not change authorization fingerprint", err)
	}
	check, err := s.Store.CheckLedger(ctx)
	if err != nil || check.InvalidReferralTerms != 0 || check.InvalidReferralRewards != 0 || check.InvalidReferralClawbacks != 0 || check.InvalidProviderRefundResults != 0 || check.InvalidJournals != 0 || check.InvalidAccounts != 0 {
		t.Fatal(fmt.Sprint(check), err)
	}
}
