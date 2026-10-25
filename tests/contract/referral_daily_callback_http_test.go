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
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/enrollment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestReferralDaily70AuthenticatedCallbackQueueAndRefundAuthorization(t *testing.T) {
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
	if _, err = s.Store.SetReferralPolicy(ctx, "global", 0, 0, payment.ReferralPolicyInput{Enabled: &enabled, RateBPS: &rate, Mode: "daily", Basis: "fee", RefundPolicy: "any_successful_refund_voids_reward"}, payment.TransferBatchActor{Role: "admin", Class: "super", ID: 1, Name: "local-admin"}); err != nil {
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
	if err != nil || balance.Available != 0 {
		t.Fatal("daily callback credited promoter before due day", balance, err)
	}
	s.Sessions = identity.NewPersistentSessions(pool, false)
	token, _, err := s.Sessions.Create(identity.User{Role: "merchant", Username: "merchant", ID: 1, AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) (int, map[string]any) {
		request := httptest.NewRequest("GET", path, nil)
		request.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, request)
		return response.Code, lookupData(t, response)
	}
	status, overview := get("/console/v1/merchant/referral/overview")
	if status != 200 || overview["daily_queue_available"] != true {
		t.Fatal("daily queue capability missing", status, overview)
	}
	status, page := get("/console/v1/merchant/referral/daily-queue?after_id=0&limit=20")
	if status != 200 {
		t.Fatal("verified own daily queue unavailable", status, page)
	}
	rows := page["items"].([]any)
	if len(rows) != 1 {
		t.Fatal("original pending entitlement missing", page)
	}
	row := rows[0].(map[string]any)
	if row["promoter_id"] != "1" || row["trade_no"] != o.TradeNo || row["state"] != "pending" || row["reward_amount"] != "5" || row["pending_amount"] != "5" || row["source_verified"] != true {
		t.Fatal("wrong verified queue projection", row)
	}
	safe, _ := json.Marshal(page)
	for _, private := range []string{"proof_hash", "request_hash", "synthetic-secret", "account_masked", "source_proof"} {
		if strings.Contains(string(safe), private) {
			t.Fatal("private financial evidence disclosed", private)
		}
	}
	for _, query := range []string{"promoter_id=1", "merchant_id=1", "after_id=0&after_id=1", "limit=101", "after_id=01"} {
		if status, _ := get("/console/v1/merchant/referral/daily-queue?" + query); status != 400 {
			t.Fatal("caller owner/noncanonical queue query accepted", query, status)
		}
	}
	target := "/console/v1/admin/orders/" + o.TradeNo + "/refund"
	actionBody := json.RawMessage(`{"amount":"0.98","external_no":"daily-callback-refund"}`)
	before, summary, err := s.ResolveFundsAction(ctx, pool, target, actionBody)
	if err != nil || summary["referral_daily_cancellation_amount"] != "0.05" || summary["referral_recovery_state"] != "daily_pending" || !strings.Contains(summary["referral_refund_policy"], "不扣推广商户可用余额") {
		t.Fatal("pending reward cancellation omitted from funds preview", summary, err)
	}
	reserved, err := s.Store.ReserveRefund(ctx, registered.MerchantID, o.OutTradeNo, "daily-callback-refund", 98)
	if err != nil {
		t.Fatal(err)
	}
	after, summary, err := s.ResolveFundsAction(ctx, pool, target, actionBody)
	if err != nil || bytes.Equal(before.Digest(), after.Digest()) || summary["referral_recovery_state"] != "daily_pending" {
		t.Fatal("funds grant not rebound after pending reserve", summary, err)
	}
	if err = s.Store.ResolveRefund(ctx, reserved.ID, "unknown"); err != nil {
		t.Fatal(err)
	}
	unknown, summary, err := s.ResolveFundsAction(ctx, pool, target, actionBody)
	if err != nil || bytes.Equal(after.Digest(), unknown.Digest()) || summary["referral_recovery_state"] != "daily_unknown" {
		t.Fatal("unknown result did not bind original pending refund evidence", summary, err)
	}
	balance, err = s.Store.Balance(ctx, 1)
	if err != nil || balance.Available != 0 || balance.Reserved != 0 {
		t.Fatal("unawarded refund borrowed promoter wallet", balance, err)
	}
}
