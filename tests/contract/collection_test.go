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

func TestCollectionCodeOwnsLinkAndCreatesUnpaidProviderOrder(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var id string
	if err := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('collection-codes',1,'{"title":"<Safe title>","name":"Test","enabled":true}') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	env, err := simulation.New(bytes.Repeat([]byte{9}, 32), "http://127.0.0.1:8180")
	if err != nil {
		t.Fatal(err)
	}
	s.CheckoutKey = bytes.Repeat([]byte{3}, 32)
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: env.Providers(), AllowLive: true, LocalSimulation: true, CallbackBaseURL: "http://127.0.0.1:8180"}
	s.MerchantChannels = map[int64][]int64{1: {1, 2}}
	token, _, _ := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	r := httptest.NewRequest("GET", "/console/v1/merchant/collection-codes/"+id+"/link", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("link %d %s", w.Code, w.Body)
	}
	var out struct {
		Data struct {
			URL string `json:"url"`
		}
	}
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	address, err := url.Parse(out.Data.URL)
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", address.RequestURI(), nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "<Safe title>") || !strings.Contains(w.Body.String(), "&lt;Safe title&gt;") {
		t.Fatalf("unsafe collection HTML: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "<h1>&lt;Safe title&gt;</h1>") {
		t.Fatal("template replaced the original collection heading")
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", address.RequestURI(), strings.NewReader(url.Values{"amount": {"1.23"}, "type": {"wxpay"}, "external_no": {"same-payer-intent"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.Handler().ServeHTTP(w, r)
	if w.Code != 302 || !strings.Contains(w.Header().Get("Location"), "/checkout/") {
		t.Fatalf("pay %d %s", w.Code, w.Body)
	}
	balance, err := s.Store.Balance(ctx, 1)
	if err != nil || balance.Available != 0 {
		t.Fatal("collection endpoint credited unpaid order")
	}
	var n int
	pool.QueryRow(ctx, "SELECT count(*) FROM provider_attempts").Scan(&n)
	if n != 1 {
		t.Fatal("provider attempt missing")
	}
	if _, err = pool.Exec(ctx, "UPDATE backoffice_records SET body=body||'{\"enabled\":false}' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", address.RequestURI(), nil))
	if w.Code != 404 {
		t.Fatal("disabled code retained access")
	}
}
