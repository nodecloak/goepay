package contract_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestSubchannel168BActualHTTPExplicitGroupMinusTwoCanBeSaved(t *testing.T) {
	s, ppool, _ := fixture(t)
	s.Sessions = identity.NewPersistentSessions(ppool, false)
	p := &groupRoutingProvider152{kind: "alipay"}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://synthetic.example.invalid"}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Role: "admin", Username: "admin", AdminClass: "super", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/console/v1/admin/resources/groups", strings.NewReader(`{"name":"subchannel168b-group","enabled":true,"info":{"alipay":{"channel":"-2","type":"channel","rate":"0"}}}`))
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("actual effective group child routing cannot be configured: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSubchannel168BActualHTTPDedicatedOwnedListDoesNotPretendLegacyRecordsExecute(t *testing.T) {
	s, ppool, _ := fixture(t)
	s.Sessions = identity.NewPersistentSessions(ppool, false)
	for _, role := range []string{"merchant", "admin"} {
		token, _, e := s.Sessions.Create(identity.User{ID: 1, Role: role, Username: role, AdminClass: "super", AuthVersion: 1})
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/payment-subchannels", nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("typed runtime child route list unavailable for %s: status=%d body=%s", role, w.Code, w.Body.String())
		}
		var out struct {
			Data struct {
				Items []map[string]any
				Total int
			}
		}
		if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Data.Items == nil || out.Data.Total != 0 {
			t.Fatalf("empty typed list must be real, not legacy metadata: %+v %v", out, e)
		}
	}
}
