package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func bindingsReq(f *email197Fixture, method, path, body string, csrf bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/merchant/account/bindings"+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if csrf {
		r.Header.Set("X-CSRF-Token", f.csrf)
	}
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func bindingsData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatal("bindings HTTP", w.Code, w.Body.String())
	}
	var out struct {
		Code int
		Data map[string]any
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Code != 0 {
		t.Fatal("bindings envelope", e, w.Body.String())
	}
	return out.Data
}

// a7 F6: mobile bind by SMS code + third-party unbind_connect, merchant session
// + CSRF, bind -> check -> unbind round-trip. The third-party BIND itself (OAuth
// link) is item (c); here we seed a bound uid and assert unbind clears it.
func TestAccountBindings134RoundTrip(t *testing.T) {
	f := email197Local(t)
	const mid int64 = 1
	const phone = "13800138000"

	// checkBind: nothing bound initially.
	v := bindingsData(t, bindingsReq(f, "GET", "", "", false))
	if v["mobile_bound"] != false || v["mobile"] != "" {
		t.Fatal("mobile should start unbound", v)
	}
	tp, ok := v["third_party"].([]any)
	if !ok || len(tp) != 4 {
		t.Fatal("third_party should list the four providers", v)
	}
	for _, row := range tp {
		if row.(map[string]any)["bound"] != false {
			t.Fatal("providers should start unbound", row)
		}
	}

	// A live bind-scene code for this merchant's phone.
	if _, e := f.p.Exec(t.Context(), `INSERT INTO verification_codes134(uid,scene,type,recipient,code,ip)VALUES($1,'bind',1,$2,'654321','203.0.113.1')`, mid, phone); e != nil {
		t.Fatal(e)
	}

	// send-code is gateway-gated: with the SMS gateway off it is 503 and issues
	// nothing (the fixture leaves the gateway unmanaged).
	if w := bindingsReq(f, "POST", "/mobile/send-code", `{"phone":"13800138000"}`, true); w.Code != 503 {
		t.Fatal("send-code must be 503 with the gateway off", w.Code, w.Body.String())
	}
	var issuedBySend int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*)FROM verification_codes134 WHERE recipient=$1`, phone).Scan(&issuedBySend); e != nil || issuedBySend != 1 {
		t.Fatal("send-code with gateway off must not issue a code", issuedBySend, e)
	}

	// Wrong code is rejected.
	if w := bindingsReq(f, "POST", "/mobile", `{"phone":"13800138000","code":"000000"}`, true); w.Code != 422 {
		t.Fatal("wrong code should be 422", w.Code, w.Body.String())
	}
	// Missing CSRF is rejected.
	if w := bindingsReq(f, "POST", "/mobile", `{"phone":"13800138000","code":"654321"}`, false); w.Code != 403 {
		t.Fatal("missing CSRF should be 403", w.Code)
	}
	// Correct code binds the mobile (masked in the response).
	bound := bindingsData(t, bindingsReq(f, "POST", "/mobile", `{"phone":"13800138000","code":"654321"}`, true))
	if bound["mobile_bound"] != true || bound["mobile"] != "138****8000" {
		t.Fatal("mobile should be bound and masked", bound)
	}
	// checkBind now reflects the bound mobile.
	if v = bindingsData(t, bindingsReq(f, "GET", "", "", false)); v["mobile_bound"] != true || v["mobile"] != "138****8000" {
		t.Fatal("checkBind should show the bound mobile", v)
	}
	// The consumed code cannot be reused (verifycode134 consume-once).
	if w := bindingsReq(f, "POST", "/mobile", `{"phone":"13800138000","code":"654321"}`, true); w.Code != 422 {
		t.Fatal("consumed code must not rebind", w.Code)
	}

	// Seed a bound QQ uid, then unbind_connect clears it.
	if _, e := f.p.Exec(t.Context(), `UPDATE merchants SET profile=COALESCE(profile,'{}'::jsonb)||jsonb_build_object('qq_uid','qq-openid-xyz')WHERE id=$1`, mid); e != nil {
		t.Fatal(e)
	}
	v = bindingsData(t, bindingsReq(f, "GET", "", "", false))
	qqBound := false
	for _, row := range v["third_party"].([]any) {
		m := row.(map[string]any)
		if m["provider"] == "qq" {
			qqBound = m["bound"] == true
		}
	}
	if !qqBound {
		t.Fatal("seeded qq uid should show bound", v)
	}
	// Missing CSRF rejected.
	if w := bindingsReq(f, "DELETE", "/connect/qq", "", false); w.Code != 403 {
		t.Fatal("unbind without CSRF should be 403", w.Code)
	}
	// Unbind clears it.
	un := bindingsData(t, bindingsReq(f, "DELETE", "/connect/qq", "", true))
	if un["bound"] != false || un["provider"] != "qq" {
		t.Fatal("unbind should report cleared", un)
	}
	// Unbinding again is a conflict (nothing bound).
	if w := bindingsReq(f, "DELETE", "/connect/qq", "", true); w.Code != 409 {
		t.Fatal("second unbind should be 409", w.Code)
	}
	// Unknown provider is 404.
	if w := bindingsReq(f, "DELETE", "/connect/weibo", "", true); w.Code != 404 {
		t.Fatal("unknown provider should be 404", w.Code)
	}

	// An audit row exists for each change.
	var audits int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*)FROM backoffice_audit WHERE resource='account-bindings' AND actor_id=$1`, mid).Scan(&audits); e != nil || audits < 2 {
		t.Fatal("bind + unbind must be audited", audits, e)
	}
}
