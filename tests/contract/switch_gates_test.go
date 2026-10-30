package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/simulation"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTotpEnrollableDefaultOffAndAdmin: GET /security/totp reports enrollable
// per the user_totp_open gate — default (no row) and admin both true, merchant
// with the switch off false.
func TestTotpEnrollableDefaultOffAndAdmin(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var err error
	s.MFA, err = identity.NewMFA(pool, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	set := func(key, value string) {
		if _, e := pool.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'=$1`, key); e != nil {
			t.Fatal(e)
		}
		if value != "" {
			if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, key, value); e != nil {
				t.Fatal(e)
			}
		}
	}
	get := func(role string) map[string]any {
		token, _, e := s.Sessions.Create(identity.User{Username: role, Role: role, ID: 1})
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/security/totp", nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s security/totp %d %s", role, w.Code, w.Body.String())
		}
		var out struct {
			Data map[string]any `json:"data"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		return out.Data
	}
	// Default (no row): merchant enrollable true.
	if d := get("merchant"); d["enrollable"] != true {
		t.Fatalf("default enrollable not true: %v", d)
	}
	// Admin is never gated.
	if d := get("admin"); d["enrollable"] != true {
		t.Fatalf("admin enrollable not true: %v", d)
	}
	// Switch off: merchant enrollable false, admin still true.
	set("user_totp_open", "0")
	if d := get("merchant"); d["enrollable"] != false {
		t.Fatalf("off enrollable not false: %v", d)
	}
	if d := get("admin"); d["enrollable"] != true {
		t.Fatalf("admin enrollable gated: %v", d)
	}
}

// TestProfitSharingGateRefusesWritesAndChannelsReason: with user_profitsharing
// off a merchant POST to rules/{channel} and receivers is refused 403, and the
// channels GET reports the feature-off reason; default (no row) is not refused
// with the feature message and the channels reason is the no-channel one.
func TestProfitSharingGateRefusesWritesAndChannelsReason(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	call := func(method, path, body string) (int, map[string]any) {
		token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	set := func(value string) {
		if _, e := pool.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'='user_profitsharing'`); e != nil {
			t.Fatal(e)
		}
		if value != "" {
			if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','user_profitsharing','value',$1::text))`, value); e != nil {
				t.Fatal(e)
			}
		}
	}
	// Default (no row): channels GET reaches the handler; the reason is the
	// no-supported-channel one, NOT the feature-off reason.
	code, out := call("GET", "/console/v1/merchant/channel-sharing/channels", "")
	if code != 200 {
		t.Fatalf("default channels %d %v", code, out)
	}
	data, _ := out["data"].(map[string]any)
	if data["reason"] == "平台未开启添加分账规则功能" {
		t.Fatalf("default channels reported feature-off reason: %v", data)
	}
	// Default: a rules POST is NOT refused with the feature 403 (it fails later
	// on provider/validation, never with the feature message).
	if code, out = call("POST", "/console/v1/merchant/channel-sharing/rules/7", `{"expected":"0","enabled":true,"minimum_amount":"0","methods":[],"items":[]}`); code == 403 && msg(out) == "未开启添加分账规则功能" {
		t.Fatal("rules POST feature-refused while user_profitsharing default ON")
	}

	// Switch off: channels GET reports the feature-off reason + available false.
	set("0")
	code, out = call("GET", "/console/v1/merchant/channel-sharing/channels", "")
	if code != 200 {
		t.Fatalf("off channels %d %v", code, out)
	}
	data, _ = out["data"].(map[string]any)
	if data["available"] != false || data["reason"] != "平台未开启添加分账规则功能" {
		t.Fatalf("off channels reason/available wrong: %v", data)
	}
	if chans, _ := data["channels"].([]any); len(chans) != 0 {
		t.Fatalf("off channels not empty: %v", data["channels"])
	}
	// Switch off: rules POST and receivers POST refused 403 with the message.
	if code, out = call("POST", "/console/v1/merchant/channel-sharing/rules/7", `{"expected":"0","enabled":true,"minimum_amount":"0","methods":[],"items":[]}`); code != 403 || msg(out) != "未开启添加分账规则功能" {
		t.Fatalf("rules POST not feature-refused when off: %d %v", code, out)
	}
	if code, out = call("POST", "/console/v1/merchant/channel-sharing/receivers", `{"channel_id":"7","external_no":"r_abcabcabcabc","type":"MERCHANT_ID","account":"x","name":"y"}`); code != 403 || msg(out) != "未开启添加分账规则功能" {
		t.Fatalf("receivers POST not feature-refused when off: %d %v", code, out)
	}
	// A read (GET rule) is never refused by the feature gate.
	if code, out = call("GET", "/console/v1/merchant/channel-sharing/rules/7", ""); code == 403 && msg(out) == "未开启添加分账规则功能" {
		t.Fatal("rule GET wrongly feature-refused when off")
	}
}

// TestPayDomainOpenGate: with pay_domain_open off, a merchant's domains schema
// reports create:false and a domains create is refused 403; the admin is never
// gated; default (no row) leaves create:true.
func TestPayDomainOpenGate(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	set := func(value string) {
		if _, e := pool.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'='pay_domain_open'`); e != nil {
			t.Fatal(e)
		}
		if value != "" {
			if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','pay_domain_open','value',$1::text))`, value); e != nil {
				t.Fatal(e)
			}
		}
	}
	call := func(role, method, path, body string) (int, map[string]any) {
		token, csrf, e := s.Sessions.Create(identity.User{Username: role, Role: role, ID: 1, AdminClass: map[string]string{"admin": "super"}[role]})
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	createFlag := func(role string) bool {
		code, out := call(role, "GET", "/console/v1/"+role+"/resources/domains/schema", "")
		if code != 200 {
			t.Fatalf("%s domains schema %d %v", role, code, out)
		}
		data, _ := out["data"].(map[string]any)
		perms, _ := data["permissions"].(map[string]any)
		c, _ := perms["create"].(bool)
		return c
	}
	deleteFlags := func() string {
		code, out := call("merchant", "GET", "/console/v1/merchant/resources/domains/schema", "")
		if code != 200 {
			t.Fatalf("merchant domains schema %d %v", code, out)
		}
		data, _ := out["data"].(map[string]any)
		perms, _ := data["permissions"].(map[string]any)
		return fmt.Sprint(perms["delete"], data["batch_delete"])
	}
	// Default (no row): merchant schema create true.
	if !createFlag("merchant") {
		t.Fatal("default merchant domains create not true")
	}
	deleteBefore := deleteFlags()
	// Default: merchant can add a domain (not feature-refused).
	if code, out := call("merchant", "POST", "/console/v1/merchant/resources/domains", `{"domain":"shop.example.com"}`); code == 403 && msg(out) == "未开启授权支付域名添加" {
		t.Fatal("domains create feature-refused while pay_domain_open default ON")
	}

	// Switch off: merchant schema create false; admin create still true.
	set("0")
	if createFlag("merchant") {
		t.Fatal("off merchant domains create still true")
	}
	if after := deleteFlags(); after != deleteBefore {
		t.Fatalf("pay_domain_open changed the merchant's delete options: %s -> %s", deleteBefore, after)
	}
	if !createFlag("admin") {
		t.Fatal("admin domains create gated by merchant switch")
	}
	// Switch off: merchant create refused 403 with the message.
	if code, out := call("merchant", "POST", "/console/v1/merchant/resources/domains", `{"domain":"shop2.example.com"}`); code != 403 || msg(out) != "未开启授权支付域名添加" {
		t.Fatalf("merchant domains create not feature-refused when off: %d %v", code, out)
	}
	// Switch off: admin create is not gated (succeeds with 201).
	if code, out := call("admin", "POST", "/console/v1/admin/resources/domains", `{"merchant_id":"1","domain":"admin-added.example.com"}`); code != 201 {
		t.Fatalf("admin domains create gated when merchant switch off: %d %v", code, out)
	}
}

// TestOnecodeDomainsGenerator: the generator lists the platform origin plus a
// configured onecode_domains extra; a generate with the extra origin produces a
// link starting with it; an unlisted origin is refused 422; default (no row) is
// the platform origin only.
func TestOnecodeDomainsGenerator(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var styleID string
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('collection-styles','{"name":"gen_std","title":"标准","enabled":true,"background_color":"#ffffff","text_color":"#000000"}') RETURNING id::text`).Scan(&styleID); e != nil {
		t.Fatal(e)
	}
	env, e := simulation.New(bytes.Repeat([]byte{9}, 32), "http://127.0.0.1:8180")
	if e != nil {
		t.Fatal(e)
	}
	s.CheckoutKey = bytes.Repeat([]byte{7}, 32)
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: env.Providers(), AllowLive: true, LocalSimulation: true, CallbackBaseURL: "http://127.0.0.1:8180"}
	s.MerchantChannels = map[int64][]int64{1: {1, 2}}
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method string, body any, anti string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/console/v1/merchant/collection-generator", bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", anti)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	setDomains := func(value string) {
		if _, e := pool.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'='onecode_domains'`); e != nil {
			t.Fatal(e)
		}
		if value != "" {
			if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','onecode_domains','value',$1::text))`, value); e != nil {
				t.Fatal(e)
			}
		}
	}
	readDomains := func(w *httptest.ResponseRecorder) []map[string]any {
		var out struct {
			Data struct {
				Domains []map[string]any `json:"domains"`
			} `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatalf("decode %s", w.Body.String())
		}
		return out.Data.Domains
	}
	// Default (no row): platform origin only.
	w := call("GET", nil, csrf)
	if w.Code != 200 {
		t.Fatalf("GET %d %s", w.Code, w.Body.String())
	}
	if d := readDomains(w); len(d) != 1 || d[0]["origin"] != "http://127.0.0.1:8180" {
		t.Fatalf("default domains not platform-only: %v", d)
	}

	// Configure an extra origin; the list is platform first then the extra.
	setDomains("https://pay.example.com\nhttp://127.0.0.1:8180\n")
	w = call("GET", nil, csrf)
	d := readDomains(w)
	if len(d) != 2 || d[0]["origin"] != "http://127.0.0.1:8180" || d[1]["origin"] != "https://pay.example.com" {
		t.Fatalf("configured domains wrong: %v", d)
	}
	// Generate with the extra origin: link starts with it.
	body := map[string]any{"external_no": "onecode-extra", "recipient_name": "本地收款", "style_id": styleID, "origin": "https://pay.example.com"}
	w = call("POST", body, csrf)
	if w.Code != 201 {
		t.Fatalf("generate with extra origin %d %s", w.Code, w.Body.String())
	}
	var gen struct {
		Data struct {
			URL string `json:"url"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &gen)
	if !strings.HasPrefix(gen.Data.URL, "https://pay.example.com/collect/") {
		t.Fatalf("link origin not the extra origin: %q", gen.Data.URL)
	}
	// An unlisted origin is refused 422.
	body2 := map[string]any{"external_no": "onecode-bad", "recipient_name": "本地收款", "style_id": styleID, "origin": "https://unlisted.example.net"}
	if w = call("POST", body2, csrf); w.Code != 422 {
		t.Fatalf("unlisted origin not refused: %d %s", w.Code, w.Body.String())
	}
	// The platform origin still generates (external_no differs).
	body3 := map[string]any{"external_no": "onecode-platform", "recipient_name": "本地收款", "style_id": styleID, "origin": "http://127.0.0.1:8180"}
	if w = call("POST", body3, csrf); w.Code != 201 {
		t.Fatalf("platform origin generate %d %s", w.Code, w.Body.String())
	}
}
