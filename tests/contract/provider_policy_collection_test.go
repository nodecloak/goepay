package contract_test

import (
	"context"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPayRollCollectionUsesStickyRoundRobinAndBrowserProducts(t *testing.T) {
	s, pool, key, calls := routingHTTPFixture(t)
	ctx := context.Background()
	rule := routingPolicy(t, s, "1", "round_robin", "7:1,8:1", "{}", "{}")
	var id string
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('collection-codes',1,'{"title":"collection routing","name":"test","enabled":true}') RETURNING id::text`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/merchant/collection-codes/"+id+"/link", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var link struct {
		Data struct {
			URL string `json:"url"`
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &link); e != nil || w.Code != 200 {
		t.Fatal(w.Code, e)
	}
	post := func(no string) {
		t.Helper()
		r := httptest.NewRequest("POST", link.Data.URL, strings.NewReader(url.Values{"amount": {"1.00"}, "type": {"alipay"}, "external_no": {no}, "allow_nonbrowser": {"true"}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 302 {
			t.Fatal("collection route failed", w.Code, w.Body.String())
		}
	}
	for i, want := range []int64{7, 8} {
		no := []string{"payer-one-intent", "payer-two-intent"}[i]
		post(no)
		post(no)
		trade := routingTrade(t, s, 1, "collect:"+id+":"+no)
		if sent := calls.list(trade); len(sent) != 1 || sent[0] != want {
			t.Fatal("collection bypassed sticky round robin", sent, want)
		}
	}
	// Collection has no preauthorization/mini completion lifecycle. A candidate
	// advertising only that product must be filtered, not selected then rejected.
	auth, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app-seven", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"preauth"}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow.Providers[7] = auth
	post("browser-only-intent")
	if sent := calls.list(routingTrade(t, s, 1, "collect:"+id+":browser-only-intent")); len(sent) != 1 || sent[0] != 8 {
		t.Fatal("collection selected an unsupported lifecycle", sent)
	}
	var assigned, attempts int
	if e = pool.QueryRow(ctx, "SELECT (SELECT assigned FROM provider_route_counters WHERE rule_id=$1),(SELECT count(*) FROM provider_attempts)", rule).Scan(&assigned, &attempts); e != nil || assigned != 3 || attempts != 3 {
		t.Fatal("collection retries consumed quota or submitted twice", assigned, attempts, e)
	}
	bal, e := s.Store.Balance(ctx, 1)
	if e != nil || bal.Available != 0 {
		t.Fatal("collection routing credited unconfirmed payment", bal, e)
	}
}
