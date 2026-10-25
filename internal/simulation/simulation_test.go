package simulation

import (
	"bytes"
	"context"
	provider "goepay/internal/adapters/payment"
	"io"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSimulationAdvertisesStableSyntheticApplicationIdentity(t *testing.T) {
	env, e := New(bytes.Repeat([]byte{9}, 32), "http://127.0.0.1:8180")
	if e != nil {
		t.Fatal(e)
	}
	for channel, p := range env.Providers() {
		identity, ok := p.(provider.ApplicationIdentity)
		if !ok || identity.ApplicationID() == "" {
			t.Fatalf("channel %d lacks explicit app identity for immutable batch bindings", channel)
		}
		if other := env.Providers()[channel].(provider.ApplicationIdentity).ApplicationID(); other != identity.ApplicationID() {
			t.Fatal("unstable simulator application identity")
		}
	}
}

func TestSimulationRequiresSignedMaterialAndVerifiedCallback(t *testing.T) {
	env, e := New(bytes.Repeat([]byte{9}, 32), "http://127.0.0.1:8180")
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	env.Notify = func(ctx context.Context, id int64, trade string, raw []byte) error {
		if _, e := env.Providers()[id].(provider.NotificationVerifier).VerifyNotification(ctx, nil, raw, provider.ExpectedPayment{TradeNo: trade, Amount: 101}); e != nil {
			return e
		}
		calls++
		return nil
	}
	p := env.Providers()[2]
	transfers, ok := p.(provider.TransferProvider)
	if !ok {
		t.Fatal("local transfer exercises unavailable")
	}
	transferRequest := provider.TransferRequest{TransferNo: "TF1", Type: "wxpay", Account: "local-only-account", Amount: 10, Remark: "本地模拟"}
	for i := 0; i < 2; i++ {
		result, err := transfers.Transfer(context.Background(), transferRequest)
		if err != nil || result.State != "SUCCESS" {
			t.Fatalf("local transfer %v %v", result, err)
		}
	}
	changed := transferRequest
	changed.Account = "changed-local-account"
	if _, err := transfers.Transfer(context.Background(), changed); err == nil {
		t.Fatal("transfer recipient mutation accepted")
	}
	if result, err := transfers.QueryTransfer(context.Background(), transferRequest); err != nil || result.Amount != 10 {
		t.Fatalf("local transfer query %v %v", result, err)
	}
	v, e := p.Create(context.Background(), provider.CreateRequest{TradeNo: "test1", Method: "native", Subject: "测试", Amount: 101})
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(v.Material)
	r := httptest.NewRequest("POST", u.RequestURI(), nil)
	w := httptest.NewRecorder()
	env.ServeHTTP(w, r)
	b, _ := io.ReadAll(w.Result().Body)
	if w.Code != 200 || calls != 1 {
		t.Fatalf("payment %d %s calls%d", w.Code, b, calls)
	}
	refundRequest := provider.RefundRequest{TradeNo: "test1", RefundNo: "local-refund", TotalAmount: 101, Amount: 10}
	for i := 0; i < 2; i++ {
		result, err := p.Refund(context.Background(), refundRequest)
		if err != nil || result.State != "SUCCESS" || result.Amount != 10 {
			t.Fatalf("local refund %v %v", result, err)
		}
	}
	refundRequest.Amount = 11
	if _, err := p.Refund(context.Background(), refundRequest); err == nil {
		t.Fatal("refund idempotency conflict accepted")
	}
	refundRequest.Amount = 10
	if result, err := p.QueryRefund(context.Background(), refundRequest); err != nil || result.State != "SUCCESS" {
		t.Fatalf("local refund query %v %v", result, err)
	}
	bad := httptest.NewRequest("POST", "/simulation/pay?token=bad", nil)
	w = httptest.NewRecorder()
	env.ServeHTTP(w, bad)
	if w.Code == 200 || calls != 1 {
		t.Fatal("tampered token accepted")
	}
}
func TestSimulationRejectsProductionBase(t *testing.T) {
	if _, e := New(make([]byte, 32), "https://pay.alipay.com"); e == nil {
		t.Fatal("production base accepted")
	}
}
