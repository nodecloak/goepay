package payment

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProviderPreflightRejectsLocalErrorsBeforeAttempt(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 1, Name: "test", APIKey: "synthetic-secret"}); e != nil {
		t.Fatal(e)
	}
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; t.Error("invalid request reached upstream") }))
	defer upstream.Close()
	wx, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: "synthetic-api-key-32-bytes-secret", Methods: []string{"jsapi", "app"}, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: wx}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	for i, tt := range []struct{ method, ip, openid string }{{"jsapi", "192.0.2.1", ""}, {"app", "not-an-ip", ""}} {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1, OutTradeNo: []string{"missing-openid", "invalid-ip"}[i], Name: "test", Type: "wxpay", Amount: 100})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = flow.Submit(ctx, o.TradeNo, 7, tt.method, tt.ip, tt.openid); !errors.Is(e, adapter.ErrInvalid) {
			t.Fatal(e)
		}
		if _, e = s.ProviderAttempt(ctx, o.TradeNo); !errors.Is(e, ErrNotFound) {
			t.Fatal("local error created durable unknown attempt", e)
		}
		order, e := s.OrderByTradeNo(ctx, o.TradeNo)
		if e != nil || order.Status != "created" {
			t.Fatal(order.Status, e)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
	var jobs int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='payment.reconcile'").Scan(&jobs); e != nil || jobs != 0 {
		t.Fatal("invalid request queued reconciliation", jobs, e)
	}
}
