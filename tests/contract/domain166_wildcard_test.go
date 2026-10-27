package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/backoffice"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDomain166WildcardHTTPApprovalOwnershipAndActualPaymentPolicy(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	type auth struct{ token, csrf string }
	tokens := map[string]auth{}
	for _, v := range []struct {
		key, role, name string
		id              int64
	}{{"admin", "admin", "admin", 1}, {"merchant", "merchant", "merchant", 1}, {"foreign", "merchant", "foreign166", 2}} {
		if v.key == "foreign" {
			if _, e := pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('foreign166','merchant',2,'synthetic','synthetic-no-login')`); e != nil {
				t.Fatal(e)
			}
		}
		a, b, e := s.Sessions.Create(identity.User{ID: v.id, Username: v.name, Role: v.role, AdminClass: "super"})
		if e != nil {
			t.Fatal(e)
		}
		tokens[v.key] = auth{a, b}
	}
	request := func(who, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		role := "merchant"
		if who == "admin" {
			role = "admin"
		}
		r := httptest.NewRequest(method, "/console/v1/"+role+"/resources/"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: tokens[who].token})
		r.Header.Set("X-CSRF-Token", tokens[who].csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	read := func(w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var v struct{ Data map[string]any }
		if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
			t.Fatal(e)
		}
		return v.Data
	}
	must := func(w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		if w.Code >= 400 {
			t.Fatalf("HTTP %d %s", w.Code, w.Body.String())
		}
		return read(w)
	}
	must(request("admin", "POST", "settings", `{"key":"enforce_domains","value":"true"}`))
	domain := must(request("merchant", "POST", "domains", `{"domain":"*.Example.COM.","enabled":true}`))
	id := fmt.Sprint(domain["id"])
	if domain["domain"] != "*.example.com" || domain["merchant_id"] != "1" || domain["approved"] != false {
		t.Fatal("wildcard/original owner/pending not retained", domain)
	}
	check := func(mid int64, rawURL string, allow bool) {
		t.Helper()
		e := backoffice.New(pool, s.Store).CheckPayment(ctx, backoffice.PaymentContext{MerchantID: mid, NotifyURL: rawURL})
		if (e == nil) != allow {
			t.Fatalf("policy mid=%d url=%q allow=%t error=%v", mid, rawURL, allow, e)
		}
	}
	check(1, "https://pay.example.com/notify", false)
	pay := func(ref, host string, allow bool) {
		t.Helper()
		_, out := call(t, s.Handler(), "POST", "/mapi.php", signed(compat.Params{"pid": "1", "out_trade_no": ref, "name": "synthetic166", "money": "1.00", "type": "sandbox", "notify_url": "https://" + host + "/notify"}, "contract-secret"))
		if (out["code"] == float64(1)) != allow {
			t.Fatal("actual payment policy", host, out)
		}
	}
	pay("domain166-pending", "pay.example.com", false)
	if w := request("merchant", "PATCH", "domains/"+id, `{"approved":true}`); w.Code != 403 {
		t.Fatal("merchant self approval", w.Code)
	}
	approved := must(request("admin", "PATCH", "domains/"+id, `{"approved":true}`))
	if approved["approved"] != true || approved["domain"] != "*.example.com" || approved["merchant_id"] != "1" {
		t.Fatal("review projection", approved)
	}
	for _, host := range []string{"pay.example.com", "deep.pay.example.com", "PAY.Example.COM.", "a-b.example.com"} {
		check(1, "https://"+host+"/notify", true)
	}
	for _, host := range []string{"example.com", "example.com.", "evil-example.com", "example.com.evil.invalid", "*.example.com", "x.*.example.com", ".example.com", "a..example.com", "-a.example.com", "a-.example.com", "pay.example.com.."} {
		check(1, "https://"+host+"/notify", false)
	}
	for _, raw := range []string{"https://pay.example.com@evil.invalid/notify", "https://evil.invalid@pay.example.com/notify", "//pay.example.com/notify", "ftp://pay.example.com/notify"} {
		check(1, raw, false)
	}
	check(2, "https://pay.example.com/notify", false)
	pay("domain166-approved", "deep.pay.example.com", true)
	var originalOrder string
	if e := pool.QueryRow(ctx, `SELECT to_jsonb(o)::text FROM orders o WHERE merchant_id=1 AND out_trade_no='domain166-approved'`).Scan(&originalOrder); e != nil {
		t.Fatal(e)
	}
	pay("domain166-root", "example.com", false)
	pay("domain166-evil", "evil-example.com", false)
	if e := backoffice.New(pool, s.Store).CheckPayment(ctx, backoffice.PaymentContext{MerchantID: 1, NotifyURL: "https://pay.example.com/notify", ReturnURL: "https://example.com/return"}); e == nil {
		t.Fatal("root return bypassed wildcard")
	}
	for _, method := range []string{"GET", "PATCH"} {
		body := ""
		if method == "PATCH" {
			body = `{"domain":"*.foreign.invalid"}`
		}
		if w := request("foreign", method, "domains/"+id, body); w.Code != 404 {
			t.Fatal("cross merchant domain", method, w.Code)
		}
	}
	must(request("merchant", "PATCH", "domains/"+id, `{"enabled":false}`))
	check(1, "https://pay.example.com/notify", false)
	must(request("merchant", "PATCH", "domains/"+id, `{"enabled":true}`))
	check(1, "https://pay.example.com/notify", true)
	changed := must(request("merchant", "PATCH", "domains/"+id, `{"domain":"*.Changed.Example.COM."}`))
	if changed["domain"] != "*.changed.example.com" || changed["approved"] != false {
		t.Fatal("changed wildcard retained approval", changed)
	}
	check(1, "https://pay.changed.example.com/notify", false)
	must(request("admin", "PATCH", "domains/"+id, `{"approved":true}`))
	check(1, "https://pay.changed.example.com/notify", true)
	check(1, "https://pay.example.com/notify", false)
	var unchangedOrder string
	if e := pool.QueryRow(ctx, `SELECT to_jsonb(o)::text FROM orders o WHERE merchant_id=1 AND out_trade_no='domain166-approved'`).Scan(&unchangedOrder); e != nil || unchangedOrder != originalOrder {
		t.Fatal("domain editing rewrote original order", e)
	}
	foreign := must(request("admin", "POST", "domains", `{"merchant_id":"2","domain":"*.Foreign.Example.COM.","approved":true,"enabled":true}`))
	if foreign["domain"] != "*.foreign.example.com" || foreign["merchant_id"] != "2" {
		t.Fatal("admin wildcard owner", foreign)
	}
	check(2, "https://a.foreign.example.com/notify", true)
	check(1, "https://a.foreign.example.com/notify", false)
	plain := must(request("admin", "POST", "domains", `{"merchant_id":"1","domain":"Legacy.Example.COM.","approved":true,"enabled":true}`))
	plainID := fmt.Sprint(plain["id"])
	var before string
	if e := pool.QueryRow(ctx, `SELECT to_jsonb(r)::text FROM backoffice_records r WHERE id=$1`, plainID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	check(1, "https://legacy.example.com/notify", true)
	check(1, "https://deep.legacy.example.com/notify", true)
	check(1, "https://evil-legacy.example.com/notify", false)
	var after string
	if e := pool.QueryRow(ctx, `SELECT to_jsonb(r)::text FROM backoffice_records r WHERE id=$1`, plainID).Scan(&after); e != nil || after != before {
		t.Fatal("policy changed original domain record", e)
	}
	schema := must(request("merchant", "GET", "domains/schema", ""))
	fields := schema["fields"].([]any)
	hint := false
	for _, raw := range fields {
		f := raw.(map[string]any)
		if f["name"] == "domain" {
			hint = strings.Contains(fmt.Sprint(f["description"]), "*.example.com")
		}
	}
	if !hint {
		t.Fatal("wildcard field hint absent")
	}
}

func TestDomain166RejectsUnsafeWildcardAndHostnameForms(t *testing.T) {
	s, pool, _ := fixture(t)
	service := backoffice.New(pool, s.Store)
	p := backoffice.Principal{Role: "merchant", SubjectID: 1, Username: "merchant"}
	for _, domain := range []string{"*", "*.com", "*example.com", "foo*.example.com", "foo.*.example.com", "*.*.example.com", ".example.com", "example..com", "*.example.com..", "*.example.com/path", "https://*.example.com", "*.example.com:443", "*.example.com?x=1", "*.example.com#x", "*.example.com@evil.invalid", "*.-bad.com", "*.bad-.com", "*._bad.com", "*.127.0.0.1", "*." + strings.Repeat("a", 64) + ".com"} {
		raw, _ := json.Marshal(domain)
		if _, e := service.Create(context.Background(), p, "domains", map[string]json.RawMessage{"domain": raw}); e == nil {
			t.Fatalf("unsafe domain accepted %q", domain)
		}
	}
}

func TestDomain166ActualFormSchemaCarriesConsumedWildcardHelp(t *testing.T) {
	s, pool, _ := fixture(t)
	for _, role := range []string{"admin", "merchant"} {
		schema, e := backoffice.New(pool, s.Store).Schema(backoffice.Principal{Role: role, SubjectID: 1, AdminClass: "super"}, "domains")
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(schema)
		t.Log("domain166Schema:" + role + ":" + string(raw))
		var v map[string]any
		if e = json.Unmarshal(raw, &v); e != nil {
			t.Fatal(e)
		}
		hint := false
		for _, raw := range v["fields"].([]any) {
			field := raw.(map[string]any)
			if field["name"] == "domain" {
				hint = strings.Contains(fmt.Sprint(field["description"]), "*.example.com")
			}
		}
		if !hint {
			t.Error("actual domain field must describe leftmost wildcard semantics", role)
		}
	}
}
