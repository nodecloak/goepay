package contract_test

import (
	"context"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"net/url"
	"testing"
)

func TestAuthorizedOrderQueryDoesNotAdvertisePayment(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "authorization-query", Name: "authorization", Type: "alipay", Amount: 125})
	if e != nil {
		t.Fatal(e)
	}
	if _, owner, e := s.Store.BeginProviderAttemptWithIdentity(ctx, o.TradeNo, 8, "preauth", "trusted-app", "buyer"); e != nil || !owner {
		t.Fatal(owner, e)
	}
	if e = s.Store.RecordPreauthorization(ctx, 8, adapter.AuthorizationEvent{TradeNo: o.TradeNo, RequestNo: o.TradeNo, AuthNo: "authorized-funds", BuyerID: "buyer", Amount: 125, State: "SUCCESS"}); e != nil {
		t.Fatal(e)
	}
	_, out := call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"order"}, "pid": {"1"}, "key": {"contract-secret"}, "trade_no": {o.TradeNo}})
	if out["status"] != float64(4) || out["api_trade_no"] != "authorized-funds" || out["buyer"] != "buyer" || out["endtime"] != "" {
		t.Fatal(out)
	}
	for _, state := range []string{"0", "4"} {
		_, list := call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"orders"}, "pid": {"1"}, "key": {"contract-secret"}, "status": {state}})
		rows, ok := list["data"].([]any)
		if !ok {
			t.Fatal(list)
		}
		want := 0
		if state == "4" {
			want = 1
		}
		if len(rows) != want {
			t.Fatal(state, list)
		}
	}
	b, e := s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatal("authorization credited platform money", b, e)
	}
	if _, owner, e := s.Store.BeginAuthorizationRelease(ctx, 1, o.TradeNo); e != nil || !owner {
		t.Fatal(owner, e)
	}
	if e = s.Store.MarkAuthorizationReleaseSent(ctx, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	// Domain tests authenticate the upstream proof; this case checks how an
	// already-confirmed release is represented in every merchant read model.
	if e = s.Store.ResolveAuthorizationRelease(ctx, o.TradeNo, "succeeded"); e != nil {
		t.Fatal(e)
	}
	_, out = call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"order"}, "pid": {"1"}, "key": {"contract-secret"}, "trade_no": {o.TradeNo}})
	if out["status"] != float64(0) || out["authorization_release_state"] != "succeeded" || out["api_trade_no"] == "authorized-funds" {
		t.Fatal("released authorization shown held", out)
	}
	_, list := call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"orders"}, "pid": {"1"}, "key": {"contract-secret"}, "status": {"4"}})
	if rows, ok := list["data"].([]any); !ok || len(rows) != 0 {
		t.Fatal("released authorization in held list", list)
	}
	check, e := s.Store.CheckLedger(ctx)
	if e != nil || check.UnsettledAuthorizations != 0 {
		t.Fatal(check, e)
	}
}
