package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestMerchantPaymentReview82HTTPLoginOriginalV1V2AndAdminCSRF(t *testing.T) {
	s, pool, key := fixture(t)
	s.Sessions = identity.NewPersistentSessions(pool, false)
	ctx := context.Background()
	h := s.Handler()
	if _, err := pool.Exec(ctx, `UPDATE merchants SET pay=2,profile=profile||'{"pay_status":1,"pay":"1"}'::jsonb`); err != nil {
		t.Fatal(err)
	}
	login := func(role, username string) (*http.Cookie, string) {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/console/v1/"+role+"/login", bytes.NewBufferString(`{"username":"`+username+`","password":"contract-password-123"}`))
		r.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(w, r)
		if w.Code != 200 || len(w.Result().Cookies()) != 1 {
			t.Fatalf("pay2 must retain normal authenticated login: %d", w.Code)
		}
		var out struct {
			Data struct {
				CSRF string `json:"csrf_token"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Data.CSRF == "" {
			t.Fatal("invalid original login response", err)
		}
		return w.Result().Cookies()[0], out.Data.CSRF
	}
	merchant, merchantCSRF := login("merchant", "merchant")
	invoke := func(method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(cookie)
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := invoke("GET", "/console/v1/merchant/account/profile", "", merchant, "")
	var profile struct {
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &profile)
	if w.Code != 200 || profile.Data["pay"] != "2" {
		t.Fatal("merchant cannot inspect original review status", w.Code, w.Body.String())
	}
	w = invoke("PATCH", "/console/v1/merchant/account/profile", `{"name":"Merchant","email":"","pay":"1"}`, merchant, merchantCSRF)
	if w.Code != 400 {
		t.Fatal("merchant escalated payment review through profile", w.Code)
	}
	base := compat.Params{"pid": "1", "out_trade_no": "review82-v1", "money": "1.25", "name": "review82", "type": "sandbox"}
	_, out := call(t, h, "POST", "/mapi.php", signed(base, "contract-secret"))
	if out["code"] != float64(-1) {
		t.Fatal("V1 bypassed review", out)
	}
	p := compat.Params{"pid": "2", "out_trade_no": "review82-v2", "money": "1.25", "name": "review82", "type": "sandbox", "sign_type": "RSA", "timestamp": fmt.Sprint(time.Now().Unix())}
	p["sign"], _ = compat.SignRSA(p, key)
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	_, out = call(t, h, "POST", "/api/pay/create", v)
	if out["code"] != float64(-1) {
		t.Fatal("V2 bypassed review", out)
	}
	_, info := call(t, h, "POST", "/api/merchant/info", v)
	if info["code"] != float64(0) || info["pay_status"] != float64(2) {
		t.Fatal("original V2 pay_status must use actual admission, not profile", info)
	}
	var orders int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM orders").Scan(&orders); err != nil || orders != 0 {
		t.Fatal("blocked API created original payment facts", orders, err)
	}
	admin, csrf := login("admin", "admin")
	w = invoke("PATCH", "/console/v1/admin/resources/merchants/1", `{"pay":"1"}`, admin, "")
	if w.Code != 403 {
		t.Fatal("review changed without CSRF", w.Code)
	}
	w = invoke("PATCH", "/console/v1/admin/resources/merchants/1", `{"pay":"1"}`, admin, csrf)
	if w.Code != 200 {
		t.Fatal("authenticated review failed", w.Code, w.Body.String())
	}
	_, out = call(t, h, "POST", "/mapi.php", signed(base, "contract-secret"))
	if out["code"] != float64(1) {
		t.Fatal("approved merchant cannot use original V1", out)
	}
	w = invoke("GET", "/console/v1/merchant/account/profile", "", merchant, "")
	json.Unmarshal(w.Body.Bytes(), &profile)
	if w.Code != 200 || profile.Data["pay"] != "1" {
		t.Fatal("review unnecessarily revoked merchant login", w.Code)
	}
	bal, err := s.Store.Balance(ctx, 1)
	if err != nil || bal.Available != 0 || bal.Reserved != 0 {
		t.Fatal("review moved funds", bal, err)
	}
}
