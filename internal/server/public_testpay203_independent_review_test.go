package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
)

// The first call deliberately crosses the real durable provider-attempt path
// before reporting an uncertain upstream outcome. No network is involved.
type publicTestPayReviewUnknown203 struct{ *channel168Provider }

func (p *publicTestPayReviewUnknown203) Create(context.Context, adapter.CreateRequest) (adapter.CreateResult, error) {
	if p.sends.Add(1) == 1 {
		return adapter.CreateResult{}, &adapter.UnknownOutcome{Operation: "controlled public test203 create", Cause: errors.New("synthetic response lost")}
	}
	return adapter.CreateResult{MaterialType: "qrcode", Material: "https://local.example.invalid/public-review203"}, nil
}

func TestPublicTestPay203IndependentReviewPreissuedFreshIntentCannotBypassPendingOriginal(t *testing.T) {
	publicTestPayReviewPreissued203(t, false)
}

func TestPublicTestPay203IndependentReviewPreissuedFreshIntentCannotBypassUnknownOriginal(t *testing.T) {
	publicTestPayReviewPreissued203(t, true)
}

func TestPublicTestPay203IndependentReviewConcurrentPreissuedFreshIntentsOnlySendOnce(t *testing.T) {
	f := publicTestPay203Ready(t)
	provider := f.s.ProviderFlow.Providers[7].(*channel168Provider)
	first, cookies := publicTestPay203ReadInit(t, f)
	secondResponse := publicTestPay203Request(f, http.MethodGet, publicTestPay203Prefix+"/init", "", cookies)
	var second struct {
		Data publicTestPay203Init `json:"data"`
	}
	if secondResponse.Code != http.StatusOK || json.Unmarshal(secondResponse.Body.Bytes(), &second) != nil || second.Data.CSRF == "" || second.Data.CSRF == first.CSRF || second.Data.TradeNo == first.TradeNo {
		t.Fatal("both concurrent callers require distinct valid preissued intents", secondResponse.Code)
	}
	financialEffects := func() string {
		t.Helper()
		var out string
		if err := f.p.QueryRow(t.Context(), `SELECT jsonb_build_object('journals',(SELECT count(*) FROM journals),'refunds',(SELECT count(*) FROM refunds),'accounts',(SELECT COALESCE(jsonb_agg(jsonb_build_array(id,balance) ORDER BY id),'[]'::jsonb) FROM accounts))::text`).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	beforeFinancial := financialEffects()
	type completion struct {
		intent publicTestPay203Init
		out    *httptest.ResponseRecorder
	}
	start := make(chan struct{})
	completed := make(chan completion, 2)
	for _, in := range []publicTestPay203Init{first, second.Data} {
		body, err := json.Marshal(map[string]string{"money": "1.23", "typeid": "700203", "csrf_token": in.CSRF})
		if err != nil {
			t.Fatal(err)
		}
		go func(intent publicTestPay203Init, body string) {
			<-start
			completed <- completion{intent, publicTestPay203Request(f, http.MethodPost, publicTestPay203Prefix+"/submit", body, cookies)}
		}(in, string(body))
	}
	close(start)
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	var winner, blocked *completion
	for range 2 {
		select {
		case result := <-completed:
			switch result.out.Code {
			case http.StatusOK:
				if winner != nil {
					t.Error("both concurrent fresh intents were accepted for provider creation")
				}
				winner = &result
			case http.StatusConflict:
				if blocked != nil {
					t.Error("both fresh intents were blocked without an accepted original")
				}
				blocked = &result
			default:
				t.Errorf("concurrent submit returned HTTP=%d want200/409", result.out.Code)
			}
		case <-deadline.C:
			t.Fatal("concurrent public submissions did not finish within the bounded HTTP contexts")
		}
	}
	if sends := provider.sends.Load(); sends != 1 {
		t.Errorf("concurrent fresh intents made %d provider sends, want1", sends)
	}
	if winner == nil || blocked == nil {
		t.Fatal("concurrent submissions must produce one accepted original and one read-only conflict")
	}
	var conflict struct {
		Data struct {
			TradeNo string `json:"trade_no"`
		} `json:"data"`
	}
	if err := json.Unmarshal(blocked.out.Body.Bytes(), &conflict); err != nil || conflict.Data.TradeNo != winner.intent.TradeNo {
		t.Errorf("blocked caller must recheck the accepted original: got=%q want=%q err=%v", conflict.Data.TradeNo, winner.intent.TradeNo, err)
	}
	var orders, attempts, reconcileJobs int
	var original, orderState, attemptState string
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts),(SELECT count(*) FROM jobs WHERE kind='payment.reconcile')`).Scan(&orders, &attempts, &reconcileJobs); err != nil || orders != 1 || attempts != 1 || reconcileJobs != 1 {
		t.Errorf("concurrent admission created extra work: orders=%d attempts=%d reconcile_jobs=%d err=%v", orders, attempts, reconcileJobs, err)
	}
	if err := f.p.QueryRow(t.Context(), `SELECT o.out_trade_no,o.status,a.state FROM orders o JOIN provider_attempts a ON a.order_id=o.id`).Scan(&original, &orderState, &attemptState); err != nil || original != winner.intent.TradeNo || orderState != "pending" || attemptState != "pending" {
		t.Errorf("accepted original must remain a single unresolved payment: original=%q order=%q attempt=%q err=%v", original, orderState, attemptState, err)
	}
	if after := financialEffects(); after != beforeFinancial {
		t.Errorf("concurrent prepares changed money or refund records: before=%s after=%s", beforeFinancial, after)
	}
}

func publicTestPayReviewPreissued203(t *testing.T, unknown bool) {
	t.Helper()
	f := publicTestPay203Ready(t)
	provider := f.s.ProviderFlow.Providers[7].(*channel168Provider)
	if unknown {
		f.s.ProviderFlow.Providers[7] = &publicTestPayReviewUnknown203{provider}
	}
	first, cookies := publicTestPay203ReadInit(t, f)
	secondResponse := publicTestPay203Request(f, http.MethodGet, publicTestPay203Prefix+"/init", "", cookies)
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("second legitimate initialization before any payment: HTTP=%d, want200", secondResponse.Code)
	}
	var second struct {
		Data publicTestPay203Init `json:"data"`
	}
	if err := json.Unmarshal(secondResponse.Body.Bytes(), &second); err != nil || second.Data.CSRF == "" || second.Data.TradeNo == "" || second.Data.CSRF == first.CSRF || second.Data.TradeNo == first.TradeNo {
		t.Fatal("two initialized public intents must have distinct original CSRF/order numbers", err)
	}
	for _, issued := range secondResponse.Result().Cookies() {
		if issued.Name == "goepay_testpay203" {
			found := false
			for _, existing := range cookies {
				if existing.Name == issued.Name && existing.Value == issued.Value {
					found = true
				}
			}
			if !found {
				t.Fatal("second initialization changed the browser identity used by this regression")
			}
		}
	}
	body := func(csrf string) string {
		raw, err := json.Marshal(map[string]string{"money": "1.23", "typeid": "700203", "csrf_token": csrf})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	firstResponse := publicTestPay203Request(f, http.MethodPost, publicTestPay203Prefix+"/submit", body(first.CSRF), cookies)
	wantHTTP, wantIntent, wantOrder, wantAttempt := http.StatusOK, "submitted", "pending", "pending"
	if unknown {
		wantHTTP, wantIntent, wantOrder, wantAttempt = http.StatusConflict, "unknown", "unknown", "unknown"
	}
	if firstResponse.Code != wantHTTP || provider.sends.Load() != 1 {
		t.Fatalf("first real controlled provider send: HTTP=%d want%d, sends=%d want1", firstResponse.Code, wantHTTP, provider.sends.Load())
	}
	var intentState, orderState, attemptState string
	if err := f.p.QueryRow(t.Context(), `SELECT i.state,o.status,a.state FROM public_testpay203_intents i JOIN orders o ON o.merchant_id=i.merchant_id AND o.out_trade_no=i.out_trade_no JOIN provider_attempts a ON a.order_id=o.id WHERE i.out_trade_no=$1`, first.TradeNo).Scan(&intentState, &orderState, &attemptState); err != nil || intentState != wantIntent || orderState != wantOrder || attemptState != wantAttempt {
		t.Fatalf("first original is genuinely unresolved: intent=%q order=%q attempt=%q, err=%v", intentState, orderState, attemptState, err)
	}
	// A newly requested intent is already blocked by the public init endpoint.
	// The preissued second intent must not bypass that same unresolved-order gate.
	if blocked := publicTestPay203Request(f, http.MethodGet, publicTestPay203Prefix+"/init", "", cookies); blocked.Code != http.StatusConflict {
		t.Fatalf("existing unresolved original must prevent a fresh payment: HTTP=%d want409", blocked.Code)
	}
	before := publicTestPay203Effects(t, f)
	secondSubmit := publicTestPay203Request(f, http.MethodPost, publicTestPay203Prefix+"/submit", body(second.Data.CSRF), cookies)
	if secondSubmit.Code != http.StatusConflict {
		t.Errorf("preissued fresh intent bypassed unresolved original: HTTP=%d want409", secondSubmit.Code)
	}
	if sends := provider.sends.Load(); sends != 1 {
		t.Errorf("preissued fresh intent sent another payment before original reconciliation: sends=%d want1", sends)
	}
	if after := publicTestPay203Effects(t, f); after != before {
		t.Errorf("preissued fresh intent added order/attempt/queue/financial effects: before=%s after=%s", before, after)
	}
}
