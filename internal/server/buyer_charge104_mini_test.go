package server

import (
	"encoding/json"
	"goepay/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuyerCharge104ActualHTTPMiniLaunchAndChildPaymentUseCharge(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	if _, e := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('buyer104-mini','merchant',1,'Synthetic mini fee merchant','unused-local-hash','operator')`); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "buyer104-mini", AdminClass: "operator", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	merchant := buyer104MerchantFixture{f, token, csrf}
	state := channel168Data(t, merchant.buyer("GET", "", ""))
	body, _ := json.Marshal(map[string]any{"mode": 1, "mode_feerate": "", "expected_fingerprint": state["fingerprint"]})
	channel168Data(t, merchant.buyer("PATCH", "", string(body)))
	global := channel168Data(t, f.buyerMinimum104("GET", "global", ""))
	body, _ = json.Marshal(map[string]any{"payfee_lessthan": "0.01", "payfee_mincost": "0.06", "expected_fingerprint": global["fingerprint"]})
	channel168Data(t, f.buyerMinimum104("PATCH", "global", string(body)))
	out := subchannel168BProductPay(t, f, "mini-actual104", "applet", nil)
	if out["code"] != float64(1) {
		t.Fatal(out)
	}
	p := factory.providers["products-v1"]
	o, e := f.s.Store.Order(t.Context(), 1, "mini-actual104")
	if e != nil {
		t.Fatal(e)
	}
	charge, e := f.s.Store.AuthenticatedOrderCharge104(t.Context(), o)
	if e != nil || charge != 106 || o.Amount != 100 {
		t.Fatal(charge, o.Amount, e)
	}
	checkout := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(checkout, httptest.NewRequest("GET", f.s.checkoutURL(o.TradeNo, "checkout"), nil))
	if checkout.Code != 200 || p.lastLaunch104.Load() != 106 || !strings.Contains(checkout.Body.String(), "1.06") {
		t.Fatal("mini material or checkout displayed original M", checkout.Code, p.lastLaunch104.Load(), checkout.Body.String())
	}
	complete := p.completeURL
	request := httptest.NewRequest("POST", complete, strings.NewReader(`{"code":"mini:products-v1"}`))
	request.Header.Set("Content-Type", "application/json")
	result := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(result, request)
	if result.Code != 200 || p.lastCreate104.Load() != 106 || p.creates.Load() != 1 {
		t.Fatal("mini child payment used original M", result.Code, result.Body.String(), p.lastCreate104.Load(), p.creates.Load())
	}
}
