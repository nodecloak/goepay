package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/simulation"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestConsoleRefundUsesProviderAndSettlesReservation(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	env, err := simulation.New(bytes.Repeat([]byte{8}, 32), "http://127.0.0.1:8180")
	if err != nil {
		t.Fatal(err)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: env.Providers(), AllowLive: true, LocalSimulation: true, CallbackBaseURL: "http://127.0.0.1:8180"}
	env.Notify = func(ctx context.Context, id int64, trade string, raw []byte) error {
		return s.ProviderFlow.HandleNotification(ctx, id, trade, nil, raw)
	}
	order, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "console-refund", Name: "Test", Type: "wxpay", Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := s.ProviderFlow.Submit(ctx, order.TradeNo, 2, "native", "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	material, _ := url.Parse(attempt.Material)
	paid := httptest.NewRecorder()
	env.ServeHTTP(paid, httptest.NewRequest("POST", material.RequestURI(), nil))
	if paid.Code != 200 {
		t.Fatal("simulation payment failed")
	}
	token, csrf, _ := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	r := httptest.NewRequest("POST", "/console/v1/merchant/orders/"+order.TradeNo+"/refund", strings.NewReader(`{"amount":"0.40","external_no":"console-refund-once"}`))
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("refund %d %s", w.Code, w.Body)
	}
	var out struct{ Data payment.Refund }
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Data.Status != "succeeded" {
		t.Fatalf("refund did not resolve %s", out.Data.Status)
	}
	balance, _ := s.Store.Balance(ctx, 1)
	if balance.Available != 60 || balance.Reserved != 0 {
		t.Fatal(balance)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM refunds").Scan(&count); err != nil || count != 1 {
		t.Fatalf("refund count%d %v", count, err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE actor_role='merchant' AND actor_id=1 AND actor_name='merchant' AND resource='orders' AND resource_id=$1 AND operation IN ('refund-intent','refund-result')", order.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("financial actor audit missing: %d %v", count, err)
	}
	if _, err = pool.Exec(ctx, `CREATE FUNCTION reject_console_intent() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='orders' AND NEW.operation='refund-intent' THEN RAISE EXCEPTION 'test audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_console_intent BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_console_intent()`); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("POST", "/console/v1/merchant/orders/"+order.TradeNo+"/refund", strings.NewReader(`{"amount":"0.10","external_no":"audit-failure"}`))
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	r.Header.Set("X-CSRF-Token", csrf)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 500 {
		t.Fatalf("missing audit did not block external request %d", w.Code)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM refunds").Scan(&count); err != nil || count != 1 {
		t.Fatal("refund started without durable actor intent")
	}
}
