package contract_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"goepay/internal/identity"
)

// The 支付方式 dropdowns (list filters + statistics pages) build their options
// from this read-only endpoint instead of a hardcoded alipay/wxpay/sandbox set.
// It returns the enabled pay types with their shownames for admin and merchant
// alike: the original defaults (alipay/wxpay open), any enabling record (so a
// configured qqpay/ecny appears), and 本地模拟 while the sandbox is on.
func TestPayTypeOptionsReflectsEnabledCatalogForBothRoles(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	// Enable QQ钱包 (default-closed) via a record, and a non-catalog ecny type.
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES
		('pay-types',jsonb_build_object('name','QQ钱包','type','qqpay','enabled',true)),
		('pay-types',jsonb_build_object('name','数字人民币','type','ecny','enabled',true)),
		('pay-types',jsonb_build_object('name','京东支付','type','jdpay','enabled',false))`); e != nil {
		t.Fatal(e)
	}
	read := func(role string, id int64) []map[string]any {
		t.Helper()
		token, _, e := s.Sessions.Create(identity.User{Username: role, ID: id, Name: role, Role: role, AdminClass: map[string]string{"admin": "super"}[role]})
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/pay-type-options", nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s pay-type-options status=%d body=%s", role, w.Code, w.Body.String())
		}
		var out struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		return out.Data.Items
	}
	for _, role := range []string{"admin", "merchant"} {
		items := read(role, 1)
		byType := map[string]string{}
		for _, it := range items {
			byType[it["value"].(string)] = it["showname"].(string)
		}
		// Default-open originals present with their shownames.
		if byType["alipay"] != "支付宝" || byType["wxpay"] != "微信支付" {
			t.Fatalf("%s: default-open originals missing: %v", role, byType)
		}
		// Enabled-by-record types present.
		if byType["qqpay"] != "QQ钱包" || byType["ecny"] != "数字人民币" {
			t.Fatalf("%s: enabled record types missing: %v", role, byType)
		}
		// A disabled record keeps the type out; default-closed stays out.
		if _, ok := byType["jdpay"]; ok {
			t.Fatalf("%s: disabled jdpay should not appear: %v", role, byType)
		}
		if _, ok := byType["bank"]; ok {
			t.Fatalf("%s: default-closed bank should not appear: %v", role, byType)
		}
		// Sandbox is offered while the test server has Sandbox on.
		if byType["sandbox"] != "本地模拟" {
			t.Fatalf("%s: sandbox option missing: %v", role, byType)
		}
	}
}

// The endpoint is GET-only, rejects query parameters, and requires a session.
func TestPayTypeOptionsMethodAndAuthGuards(t *testing.T) {
	s, _, _ := fixture(t)
	token, csrf, e := s.Sessions.Create(identity.User{Username: "admin", ID: 1, Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	do := func(method, path, cookie string) int {
		r := httptest.NewRequest(method, path, nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: cookie})
		}
		if method != "GET" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if code := do("GET", "/console/v1/admin/pay-type-options", ""); code != 401 {
		t.Fatalf("unauthenticated accepted: %d", code)
	}
	if code := do("POST", "/console/v1/admin/pay-type-options", token); code != 405 {
		t.Fatalf("POST accepted: %d", code)
	}
	if code := do("GET", "/console/v1/admin/pay-type-options?type=alipay", token); code != 400 {
		t.Fatalf("query parameters accepted: %d", code)
	}
}
