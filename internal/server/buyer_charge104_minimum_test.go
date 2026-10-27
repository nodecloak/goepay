package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func (f channel168Fixture) buyerMinimum104(method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/buyer-fees/"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func TestBuyerCharge104OriginalMinimumGlobalParentInheritAndZero(t *testing.T) {
	f := channel168Local(t)
	v := channel168Data(t, f.buyerMinimum104("GET", "global", ""))
	body, _ := json.Marshal(map[string]any{"payfee_lessthan": "0.10", "payfee_mincost": "0.20", "expected_fingerprint": v["fingerprint"]})
	v = channel168Data(t, f.buyerMinimum104("PATCH", "global", string(body)))
	if v["payfee_lessthan"] != "0.10" || v["payfee_mincost"] != "0.20" {
		t.Fatal("original minimum fields lost", v)
	}
	parent := channel168Data(t, f.buyerMinimum104("GET", "parents/7", ""))
	if parent["payfee_lessthan"] != "" || parent["payfee_mincost"] != "" || parent["effective_payfee_lessthan"] != "0.10" || parent["effective_payfee_mincost"] != "0.20" {
		t.Fatal("parent blank must inherit", parent)
	}
	body, _ = json.Marshal(map[string]any{"payfee_lessthan": "0", "payfee_mincost": "", "expected_fingerprint": parent["fingerprint"]})
	parent = channel168Data(t, f.buyerMinimum104("PATCH", "parents/7", string(body)))
	if parent["payfee_lessthan"] != "0.00" || parent["effective_payfee_lessthan"] != "0.10" || parent["effective_payfee_mincost"] != "0.20" {
		t.Fatal("parent zero must retain the global pair", parent)
	}
	if w := f.buyerMinimum104("PATCH", "parents/7", string(body)); w.Code != 409 {
		t.Fatal("old minimum confirmation accepted", w.Code, w.Body.String())
	}
	if w := f.buyerMinimum104("GET", "parents/999", ""); w.Code != 404 {
		t.Fatal("unregistered fee target", w.Code)
	}
	var sideEffects int
	if e := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM orders)+(SELECT count(*) FROM provider_attempts)+(SELECT count(*) FROM journals)`).Scan(&sideEffects); e != nil || sideEffects != 0 {
		t.Fatal("minimum setting moved money", sideEffects, e)
	}
}
