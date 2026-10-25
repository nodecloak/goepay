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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
)

type referralClawbackTLS struct {
	Store          *Store
	Flow           ProviderFlow
	Order          Order
	Phase          atomic.Int32 // 0 lost acceptance, 1 wrong amount, 2 wrong trade, 3 forged signature, 4 success, 5 closed, 6 accepted pending
	Sends, Queries atomic.Int32
}

func newReferralClawbackTLS(t *testing.T) *referralClawbackTLS {
	t.Helper()
	ctx := context.Background()
	s := referralClawbackSetup(t, "platform")
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 902, OutTradeNo: "tls-reward-payment", Name: "local original TLS payment", Type: "wxpay", Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	f := &referralClawbackTLS{Store: s, Order: o}
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	requests := map[string]int64{}
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var out any
		switch {
		case r.Method == "POST" && r.URL.Path == "/v3/pay/transactions/native":
			out = map[string]any{"code_url": "weixin://wxpay/bizpayurl?pr=local-referral-original"}
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v3/pay/transactions/out-trade-no/"):
			out = map[string]any{"appid": "local-referral-app", "mchid": "local-referral-mch", "out_trade_no": o.TradeNo, "transaction_id": "WX_" + o.TradeNo, "trade_state": "SUCCESS", "amount": map[string]any{"total": 100, "currency": "CNY"}, "payer": map[string]string{"openid": "local-buyer"}}
		case r.Method == "POST" && r.URL.Path == "/v3/refund/domestic/refunds":
			f.Sends.Add(1)
			var input struct {
				TradeNo  string `json:"out_trade_no"`
				RefundNo string `json:"out_refund_no"`
				Amount   struct {
					Total, Refund int64
					Currency      string
				} `json:"amount"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.TradeNo != o.TradeNo || input.Amount.Total != 100 || input.Amount.Currency != "CNY" || !strings.HasPrefix(input.RefundNo, "RF") {
				t.Error("lost original refund identity", input, err)
				w.WriteHeader(400)
				return
			}
			mu.Lock()
			requests[input.RefundNo] = input.Amount.Refund
			mu.Unlock()
			if f.Phase.Load() == 0 {
				w.WriteHeader(503)
				return
			}
			out = map[string]any{"out_trade_no": o.TradeNo, "transaction_id": "WX_" + o.TradeNo, "out_refund_no": input.RefundNo, "refund_id": "WX_" + input.RefundNo, "status": "PROCESSING", "amount": map[string]any{"total": 100, "refund": input.Amount.Refund, "currency": "CNY"}}
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v3/refund/domestic/refunds/"):
			f.Queries.Add(1)
			no := strings.TrimPrefix(r.URL.Path, "/v3/refund/domestic/refunds/")
			mu.Lock()
			amount, exists := requests[no]
			mu.Unlock()
			if !exists {
				t.Error("query changed original refund number", no)
				w.WriteHeader(400)
				return
			}
			trade, state := o.TradeNo, "SUCCESS"
			if f.Phase.Load() == 1 {
				amount++
			}
			if f.Phase.Load() == 2 {
				trade = "FOREIGN_ORIGINAL"
			}
			if f.Phase.Load() == 5 {
				state = "CLOSED"
			}
			if f.Phase.Load() == 6 {
				state = "PROCESSING"
			}
			out = map[string]any{"out_trade_no": trade, "transaction_id": "WX_" + o.TradeNo, "out_refund_no": no, "refund_id": "WX_" + no, "status": state, "amount": map[string]any{"total": 100, "refund": amount, "currency": "CNY"}}
		default:
			t.Error("unexpected local refund endpoint", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		raw, _ := json.Marshal(out)
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := "local-referral-clawback"
		hash := sha256.Sum256([]byte(stamp + "\n" + nonce + "\n" + string(raw) + "\n"))
		signature, err := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, hash[:])
		if err != nil {
			t.Error(err)
			return
		}
		if f.Phase.Load() == 3 && strings.HasPrefix(r.URL.Path, "/v3/refund/") {
			signature[0] ^= 1
		}
		w.Header().Set("Wechatpay-Timestamp", stamp)
		w.Header().Set("Wechatpay-Nonce", nonce)
		w.Header().Set("Wechatpay-Serial", "PUB_KEY_ID_LOCAL_REFERRAL")
		w.Header().Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(signature))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(up.Close)
	p, err := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-referral-app", MerchantID: "local-referral-mch", MerchantSerial: "LOCAL_REFERRAL_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL_REFERRAL": &platform.PublicKey}, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: sharingReceiverTLSTransport{up}}})
	if err != nil {
		t.Fatal(err)
	}
	f.Flow = ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://local.example.invalid"}
	if _, err = f.Flow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Flow.Query(ctx, 902, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestReferralClawbackActualWechatTLSUnknownProofAndAtomicQueryRecovery(t *testing.T) {
	f := newReferralClawbackTLS(t)
	ctx := context.Background()
	if _, err := f.Flow.Refund(ctx, 902, f.Order.TradeNo, "tls-unknown", 98); err == nil {
		t.Fatal("lost receipt accepted")
	}
	referralClawbackBalances(t, f.Store, 0, 8, 2, 0)
	for _, phase := range []int32{1, 2, 3} {
		f.Phase.Store(phase)
		if _, err := f.Flow.QueryRefund(ctx, 902, "tls-unknown"); !errors.Is(err, adapter.ErrAuthentication) {
			t.Fatalf("adopted untrusted result phase%d: %v", phase, err)
		}
		referralClawbackBalances(t, f.Store, 0, 8, 2, 0)
	}
	f.Phase.Store(4)
	if _, err := f.Store.pool.Exec(ctx, `CREATE FUNCTION fail_referral_resolution_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='succeeded' THEN RAISE EXCEPTION 'local forced result audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_referral_resolution_test BEFORE INSERT ON referral_clawback_audits FOR EACH ROW EXECUTE FUNCTION fail_referral_resolution_test()`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Flow.QueryRefund(ctx, 902, "tls-unknown"); err == nil {
		t.Fatal("result without audit committed")
	}
	r, err := f.Store.Refund(ctx, 902, "tls-unknown")
	if err != nil || r.Status != "unknown" {
		t.Fatalf("partial result committed %+v %v", r, err)
	}
	referralClawbackBalances(t, f.Store, 0, 8, 2, 0)
	if _, err = f.Store.pool.Exec(ctx, "DROP TRIGGER fail_referral_resolution_test ON referral_clawback_audits"); err != nil {
		t.Fatal(err)
	}
	r, err = f.Flow.QueryRefund(ctx, 902, "tls-unknown")
	if err != nil || r.Status != "succeeded" {
		t.Fatalf("original query recovery %+v %v", r, err)
	}
	referralClawbackBalances(t, f.Store, 2, 0, 0, 0)
	if _, err = f.Flow.QueryRefund(ctx, 902, "tls-unknown"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Flow.Refund(ctx, 902, f.Order.TradeNo, "tls-unknown", 98); err != nil {
		t.Fatal(err)
	}
	if f.Sends.Load() != 1 || f.Queries.Load() != 5 {
		t.Fatal("recovery resent financial request", f.Sends.Load(), f.Queries.Load())
	}
	var n int
	if err = f.Store.pool.QueryRow(ctx, "SELECT count(*) FROM journals WHERE event_key=$1", fmt.Sprintf("referral-clawback:%d:release-remainder", r.ID)).Scan(&n); err != nil || n != 1 {
		t.Fatal("reward revoked more than once", n, err)
	}
	report, err := f.Store.CheckLedger(ctx)
	if err != nil || report.InvalidReferralClawbacks != 0 || report.InvalidReferralRewards != 0 || report.InvalidReferralTerms != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatalf("unbalanced reward recovery %+v %v", report, err)
	}
}
