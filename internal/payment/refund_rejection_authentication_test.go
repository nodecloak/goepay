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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

type rejectingRefundProvider struct {
	operationsProvider
	failure error
	refund  func(context.Context, adapter.RefundRequest) (adapter.RefundResult, error)
}

func (p *rejectingRefundProvider) Refund(ctx context.Context, r adapter.RefundRequest) (adapter.RefundResult, error) {
	p.refunds.Add(1)
	if p.refund != nil {
		return p.refund(ctx, r)
	}
	return adapter.RefundResult{}, p.failure
}

func TestRefundUnverifiedRejectionKeepsReservation(t *testing.T) {
	for _, mode := range []string{"plain-error", "unknown-wrapper", "cancelled-wrapper", "plaintext-response", "authenticated-response"} {
		t.Run(mode, func(t *testing.T) {
			s, flow, _, order := setupOperations(t)
			ctx := context.Background()
			if _, err := flow.Query(ctx, 1, order.TradeNo); err != nil {
				t.Fatal(err)
			}
			p := &rejectingRefundProvider{failure: &adapter.ProviderError{Code: "NOTENOUGH"}}
			switch mode {
			case "unknown-wrapper":
				p.failure = &adapter.UnknownOutcome{Operation: "refund", Cause: p.failure}
			case "cancelled-wrapper":
				p.failure = errors.Join(context.Canceled, p.failure)
			case "plaintext-response", "authenticated-response":
				const key = "synthetic-refund-rejection-key"
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/secapi/pay/refund" {
						t.Errorf("unexpected refund path: %s", r.URL.Path)
					}
					writeScanWechat78(w, map[string]string{"return_code": "SUCCESS", "result_code": "FAIL", "err_code": "NOTENOUGH", "appid": "refund-app", "mch_id": "refund-mch", "nonce_str": "refund-rejection"}, key)
				})
				up := httptest.NewUnstartedServer(handler)
				if mode == "authenticated-response" {
					up.StartTLS()
				} else {
					up.Start()
				}
				defer up.Close()
				provider, err := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "refund-app", MerchantID: "refund-mch", APIKey: key, BaseURL: up.URL, HTTPClient: up.Client(), RefundClient: up.Client()})
				if err != nil {
					t.Fatal(err)
				}
				p.refund = provider.Refund
			}
			flow.Providers[7] = p
			refund, err := flow.Refund(ctx, 1, order.TradeNo, "untrusted-rejection", 60)
			if err == nil || p.refunds.Load() != 1 {
				t.Fatal("fixture must reach exactly one rejected refund", p.refunds.Load(), err)
			}
			refund, err = s.Refund(ctx, 1, refund.RefundNo)
			if err != nil {
				t.Fatal(err)
			}
			balance, err := s.Balance(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "authenticated-response" {
				if refund.Status != "failed" || balance.Available != 100 || balance.Reserved != 0 {
					t.Fatal("authenticated definitive rejection did not release funds", refund.Status, balance)
				}
			} else if refund.Status != "unknown" || balance.Available != 40 || balance.Reserved != 60 {
				t.Fatal("unverified rejection released possibly spent funds", refund.Status, balance)
			}
			_, _ = flow.Refund(ctx, 1, order.TradeNo, refund.RefundNo, 60)
			if p.refunds.Load() != 1 {
				t.Fatal("same-number retry resent refund")
			}
			if mode != "authenticated-response" {
				current, queryErr := flow.QueryRefund(ctx, 1, refund.RefundNo)
				if queryErr != nil || current.Status != "succeeded" {
					t.Fatal("original refund could not reconcile", current.Status, queryErr)
				}
			}
		})
	}
}

func TestCombineRefundUnverifiedRejectionKeepsShareUnknown(t *testing.T) {
	s, order := combineRefund207Paid(t)
	ctx := context.Background()
	r, err := s.ReserveRefund(ctx, order.MerchantID, order.OutTradeNo, "untrusted-combine-rejection", 1000)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.planCombineRefund207(ctx, r.ID, order.ID, 1000, "untrusted-combine")
	if err != nil {
		t.Fatal(err)
	}
	p := &rejectingRefundProvider{failure: &adapter.UnknownOutcome{Operation: "refund", Cause: &adapter.ProviderError{Code: "PARAM_ERROR"}}}
	state, err := (ProviderFlow{Store: s}).runCombineRefund(ctx, p, r.ID, plan, true)
	if state != "unknown" || err == nil || p.refunds.Load() != 1 {
		t.Fatal("ambiguous combined refund share became terminal", state, err, p.refunds.Load())
	}
}

func TestRefundRejectionRequiresOriginalAuthenticatedOperation(t *testing.T) {
	const key = "synthetic-refund-operation-key"
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeScanWechat78(w, map[string]string{"return_code": "SUCCESS", "result_code": "FAIL", "err_code": "NOTENOUGH", "appid": "refund-app", "mch_id": "refund-mch", "nonce_str": "refund-rejection"}, key)
	}))
	defer up.Close()
	p, err := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "refund-app", MerchantID: "refund-mch", APIKey: key, BaseURL: up.URL, HTTPClient: up.Client(), RefundClient: up.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	request := adapter.RefundRequest{TradeNo: "order", RefundNo: "RF1", TotalAmount: 100, Amount: 60}
	_, refused := p.Refund(ctx, request)
	_, queryErr := p.QueryRefund(ctx, request)
	closeErr := p.Close(ctx, adapter.QueryRequest{TradeNo: "order"})
	if _, valid := adapter.VerifiedProviderRejection(refused); !valid {
		t.Fatal("fixture requires authenticated TLS rejection", refused)
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"original-refund", refused, true},
		{"unknown-overrides-authentication", &adapter.UnknownOutcome{Operation: "refund", Cause: refused}, false},
		{"cancelled-overrides-authentication", errors.Join(refused, context.Canceled), false},
		{"deadline-overrides-authentication", errors.Join(refused, context.DeadlineExceeded), false},
		{"query-is-not-submit", queryErr, false},
		{"close-is-not-refund", closeErr, false},
		{"code-has-no-provenance", &adapter.ProviderError{Code: "NOTENOUGH"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := definiteAuthenticatedRefundRejection(tc.err, p); got != tc.want {
				t.Fatal("incorrect refund release eligibility", got, tc.err)
			}
		})
	}
	insecure := up.Client().Transport.(*http.Transport).Clone()
	insecure.TLSClientConfig = insecure.TLSClientConfig.Clone()
	insecure.TLSClientConfig.InsecureSkipVerify = true // Synthetic negative case only.
	defer insecure.CloseIdleConnections()
	unsafeClient := &http.Client{Transport: insecure}
	unsafe, err := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "refund-app", MerchantID: "refund-mch", APIKey: key, BaseURL: up.URL, HTTPClient: unsafeClient, RefundClient: unsafeClient})
	if err != nil {
		t.Fatal(err)
	}
	_, unsafeErr := unsafe.Refund(ctx, request)
	if definiteAuthenticatedRefundRejection(unsafeErr, unsafe) {
		t.Fatal("TLS without certificate verification authorized a release")
	}

	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	aliUp := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		node := []byte(`{"code":"40002","sub_code":"isv.invalid-parameter"}`)
		digest := sha256.Sum256(node)
		signature, err := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		if err != nil {
			t.Error(err)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(r.Form.Get("method"), ".", "_") + "_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(signature)})
	}))
	defer aliUp.Close()
	ali, err := adapter.NewAlipay(adapter.AlipayConfig{AppID: "refund-app", SellerID: "2088123456789012", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: aliUp.URL, HTTPClient: aliUp.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, aliErr := ali.Refund(ctx, request)
	if !definiteAuthenticatedRefundRejection(aliErr, ali) {
		t.Fatal("authenticated Alipay refund refusal was lost", aliErr)
	}
	if definiteAuthenticatedRefundRejection(aliErr, p) || definiteAuthenticatedRefundRejection(refused, ali) {
		t.Fatal("another provider's refusal authorized refund release")
	}
	if definiteAuthenticatedRefundRejection(ali.Close(ctx, adapter.QueryRequest{TradeNo: "order"}), ali) {
		t.Fatal("Alipay close refusal authorized refund release")
	}
}
