package contract_test

import (
	"bytes"
	"context"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/simulation"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRechargeAppliesBuyerBlacklistBeforeProviderAttempt(t *testing.T) {
	s, pool, _ := fixture(t)
	env, err := simulation.New(bytes.Repeat([]byte{4}, 32), "http://127.0.0.1:8180")
	if err != nil {
		t.Fatal(err)
	}
	s.CheckoutKey = bytes.Repeat([]byte{5}, 32)
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: env.Providers(), AllowLive: true, LocalSimulation: true, CallbackBaseURL: "http://127.0.0.1:8180"}
	s.MerchantChannels = map[int64][]int64{1: {2}}
	if _, err = pool.Exec(context.Background(), `INSERT INTO backoffice_records(resource,body) VALUES('blacklist','{"kind":"buyer","value":"blocked-openid","reason":"test","enabled":true}')`); err != nil {
		t.Fatal(err)
	}
	token, csrf, _ := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	r := httptest.NewRequest("POST", "/console/v1/merchant/workflows/recharges", strings.NewReader(`{"amount":"1.00","external_no":"blocked-buyer","type":"wxpay","method":"native","open_id":"blocked-openid"}`))
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var n int
	if err = pool.QueryRow(context.Background(), "SELECT count(*) FROM provider_attempts").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 || w.Code == 201 {
		t.Fatalf("blacklisted buyer reached provider: attempts=%d status=%d", n, w.Code)
	}
	if err = pool.QueryRow(context.Background(), "SELECT count(*) FROM orders").Scan(&n); err != nil || n != 0 {
		t.Fatalf("blacklisted recharge created an order: %d %v", n, err)
	}
}

func TestRechargeRejectsUnsupportedSceneAndChannelBeforeOrderCreation(t *testing.T) {
	s, pool, _ := fixture(t)
	env, e := simulation.New(bytes.Repeat([]byte{4}, 32), "http://127.0.0.1:8180")
	if e != nil {
		t.Fatal(e)
	}
	s.CheckoutKey = bytes.Repeat([]byte{5}, 32)
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: env.Providers(), AllowLive: true, LocalSimulation: true, CallbackBaseURL: "http://127.0.0.1:8180"}
	s.MerchantChannels = map[int64][]int64{1: {2}}
	token, csrf, _ := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	for _, body := range []string{`{"amount":"1.00","external_no":"app","type":"wxpay","method":"app"}`, `{"amount":"1.00","external_no":"jsapi","type":"wxpay","method":"jsapi","open_id":"untrusted"}`, `{"amount":"1.00","external_no":"channel","type":"wxpay","method":"native","channel_id":"999"}`} {
		r := httptest.NewRequest("POST", "/console/v1/merchant/workflows/recharges", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 422 {
			t.Fatalf("known local rejection should be422, got%d %s", w.Code, w.Body.String())
		}
	}
	var orders, intents, attempts int
	if e = pool.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM recharge_intents),(SELECT count(*) FROM provider_attempts)").Scan(&orders, &intents, &attempts); e != nil || orders != 0 || intents != 0 || attempts != 0 {
		t.Fatalf("local rejection persisted order/intent/attempt %d,%d,%d %v", orders, intents, attempts, e)
	}
}
