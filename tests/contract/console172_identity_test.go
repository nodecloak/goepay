package contract_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/payment"
)

func TestPersonal172RealConsoleLoginAndSessionPreserveLargeOwnerID(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	const id int64 = 9007199254740993
	if e := f.s.Store.CreateMerchant(ctx, payment.Merchant{ID: id, Name: "synthetic172-large-owner", APIKey: "synthetic172-only"}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(ctx, "UPDATE console_users SET subject_id=$1,auth_version=auth_version+1 WHERE username='merchant'", id); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/console/v1/merchant/login", strings.NewReader(`{"username":"merchant","password":"contract-password-123"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("large original owner login failed", w.Code)
	}
	check := func(w *httptest.ResponseRecorder) {
		t.Helper()
		data := credentialData162(t, w)
		user, ok := data["user"].(map[string]any)
		if !ok {
			t.Fatal("console user absent")
		}
		if user["id"] != "9007199254740993" {
			t.Fatalf("console owner ID type=%T; complete string required", user["id"])
		}
		if user["role"] != "merchant" || user["name"] != "Merchant" {
			t.Fatal("console identity semantics changed")
		}
		for _, key := range []string{"username", "auth_version", "password", "api_key"} {
			if _, ok := user[key]; ok {
				t.Fatal("console identity exposed internal field", key)
			}
		}
	}
	check(w)
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "goepay_merchant" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("original merchant cookie absent")
	}
	r = httptest.NewRequest("GET", "/console/v1/merchant/session", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("large original session failed", w.Code)
	}
	check(w)
	// Consume the real login cookie, not a manually constructed JavaScript fixture.
	r = httptest.NewRequest("GET", "/console/v1/merchant/account/personal-settings", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("large own settings unavailable", w.Code)
	}
	if data := credentialData162(t, w); data["merchant_id"] != "9007199254740993" {
		t.Fatal("large settings owner changed")
	}
}
