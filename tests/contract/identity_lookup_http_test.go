package contract_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func lookupHTTP(t *testing.T, h http.Handler, method, path, body, token, role, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("lookup response can be cached/referred", w.Header())
	}
	return w
}
func lookupData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &envelope); e != nil {
		t.Fatal(w.Code, w.Body.String(), e)
	}
	return envelope.Data
}

func TestIdentityLookupHTTPRealTLSAdaptersNoPaymentAndExplicitOneTimeReveal(t *testing.T) {
	for _, kind := range []string{"wxpay", "alipay"} {
		t.Run(kind, func(t *testing.T) {
			s, pool, key := fixture(t)
			s.Sandbox = false
			s.Sessions = identity.NewPersistentSessions(pool, true)
			token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
			if e != nil {
				t.Fatal(e)
			}
			platform, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}
			var exchanges atomic.Int32
			var fail atomic.Bool
			var onExchange func()
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				exchanges.Add(1)
				if onExchange != nil {
					onExchange()
				}
				if fail.Load() {
					w.WriteHeader(503)
					return
				}
				if kind == "wxpay" {
					if r.URL.Path != "/sns/oauth2/access_token" || r.URL.Query().Get("appid") != "lookup-app" {
						t.Error("lookup used payment/foreign API", r.URL.Path)
						w.WriteHeader(500)
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"access_token": "discard-token", "expires_in": 7200, "scope": "snsapi_base", "openid": "private-lookup-openid"})
					return
				}
				r.ParseForm()
				if r.Form.Get("method") != "alipay.system.oauth.token" || r.Form.Get("app_id") != "lookup-app" {
					t.Error("lookup used payment/foreign API", r.Form.Get("method"))
					w.WriteHeader(500)
					return
				}
				node, _ := json.Marshal(map[string]any{"open_id": "private-lookup-openid", "access_token": "discard-token", "expires_in": "3600"})
				digest := sha256.Sum256(node)
				sig, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
				json.NewEncoder(w).Encode(map[string]any{"alipay_system_oauth_token_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(sig)})
			}))
			defer up.Close()
			var p adapter.Provider
			var oauth adapter.WebOAuthProvider
			if kind == "wxpay" {
				client := &http.Client{Transport: oauthWechatTLSTransport{up, t}}
				p, e = adapter.NewWechatV2(adapter.WechatV2Config{AppID: "lookup-app", MerchantID: "lookup-mch", APIKey: "0123456789abcdef0123456789abcdef", Methods: []string{"jsapi"}, HTTPClient: client})
				if e != nil {
					t.Fatal(e)
				}
				oauth, e = adapter.NewWechatOAuth(adapter.WechatOAuthConfig{AppID: "lookup-app", AppSecret: "synthetic-secret", HTTPClient: client})
			} else {
				ali, err := adapter.NewAlipay(adapter.AlipayConfig{AppID: "lookup-app", PrivateKey: key, PublicKey: &platform.PublicKey, Methods: []string{"jsapi"}, HTTPClient: &http.Client{Transport: oauthContractTLSTransport{up, t}}})
				p, oauth, e = ali, ali, err
			}
			if e != nil {
				t.Fatal(e)
			}
			s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
			s.OAuthProviders = map[int64]adapter.WebOAuthProvider{7: oauth}
			s.MerchantChannels = map[int64][]int64{1: {7}}
			h := s.Handler()
			base := "/console/v1/merchant/identity-lookups"
			capabilities := lookupHTTP(t, h, "GET", base+"/capabilities", "", token, "merchant", csrf)
			if capabilities.Code != 200 || lookupData(t, capabilities)["available"] != true || exchanges.Load() != 0 {
				t.Fatal("read-only capabilities", capabilities.Code, capabilities.Body.String())
			}
			for _, query := range []string{"?merchant_id=2", "?merchant_id=1&merchant_id=1", "?sid=anything"} {
				if w := lookupHTTP(t, h, "GET", base+"/capabilities"+query, "", token, "merchant", csrf); w.Code < 400 {
					t.Fatal("invalid capability query", query)
				}
			}
			s.Sandbox = true
			capabilities = lookupHTTP(t, h, "GET", base+"/capabilities", "", token, "merchant", csrf)
			if capabilities.Code != 200 || lookupData(t, capabilities)["available"] != false || lookupData(t, capabilities)["reason"] == "" {
				t.Fatal("sandbox capability missing reason", capabilities.Body.String())
			}
			s.Sandbox = false
			create := func() map[string]any {
				t.Helper()
				w := lookupHTTP(t, h, "POST", base, `{"channel_id":"7"}`, token, "merchant", csrf)
				if w.Code != 201 {
					t.Fatal(w.Code, w.Body.String())
				}
				return lookupData(t, w)
			}
			start := func(data map[string]any) (string, string, *http.Cookie) {
				t.Helper()
				w := lookupHTTP(t, h, "GET", data["authorization_url"].(string), "", "", "", "")
				if w.Code != 302 {
					t.Fatal(w.Code, w.Body.String())
				}
				u, e := url.Parse(w.Header().Get("Location"))
				if e != nil {
					t.Fatal(e)
				}
				cookies := w.Result().Cookies()
				if len(cookies) != 1 || cookies[0].Name != "__Host-goepay_identity" || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
					t.Fatal("unsafe browser binding", cookies)
				}
				return u.Query().Get("redirect_uri"), u.Query().Get("state"), cookies[0]
			}
			callback := func(path, state, code string, c *http.Cookie) *httptest.ResponseRecorder {
				t.Helper()
				u, _ := url.Parse(path)
				q := url.Values{"state": {state}, "app_id": {"lookup-app"}}
				if kind == "wxpay" {
					q.Set("code", code)
					q.Set("scope", "snsapi_base")
				} else {
					q.Set("auth_code", code)
					q.Set("scope", "auth_base")
				}
				u.RawQuery = q.Encode()
				r := httptest.NewRequest("GET", u.String(), nil)
				if c != nil {
					r.AddCookie(c)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
					t.Fatal("callback headers")
				}
				return w
			}
			for _, body := range []string{`{"channel_id":"7","merchant_id":"2"}`, `{"channel_id":"7","channel_id":"7"}`, `{"channel_id":"7","sid":"attacker"}`, `{"channel_id":7}`} {
				if w := lookupHTTP(t, h, "POST", base, body, token, "merchant", csrf); w.Code < 400 {
					t.Fatal("invalid create accepted", body)
				}
			}
			if w := lookupHTTP(t, h, "POST", base, `{"channel_id":"7"}`, token, "merchant", "bad-csrf"); w.Code != 403 {
				t.Fatal("CSRF omitted", w.Code)
			}
			data := create()
			path, state, cookie := start(data)
			id := data["request_id"].(string)
			if strings.Contains(data["authorization_url"].(string), token) || exchanges.Load() != 0 {
				t.Fatal("session in link or premature exchange")
			}
			if w := callback(path, state, "one-code", nil); w.Code < 400 {
				t.Fatal("unbound browser accepted")
			}
			bad := httptest.NewRequest("GET", path+"?state="+state+"&state="+state+"&code=code", nil)
			bad.AddCookie(cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, bad)
			if w.Code < 400 || exchanges.Load() != 0 {
				t.Fatal("duplicate query exchanged")
			}
			if w := callback(path, state, "one-code", cookie); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := callback(path, state, "two-code", cookie); w.Code < 400 || exchanges.Load() != 1 {
				t.Fatal("replayed callback exchanged")
			}
			w = lookupHTTP(t, h, "GET", base+"/"+id, "", token, "merchant", csrf)
			if w.Code != 200 || strings.Contains(w.Body.String(), "private-lookup-openid") || lookupData(t, w)["status"] != "verified" {
				t.Fatal("unsafe metadata", w.Code, w.Body.String())
			}
			w = lookupHTTP(t, h, "POST", base+"/"+id+"/reveal", `{}`, token, "merchant", csrf)
			got := lookupData(t, w)
			if w.Code != 200 || got["subject_id"] != "private-lookup-openid" || got["label"] == "支付宝账号" {
				t.Fatal("reveal result", w.Code, got)
			}
			if w = lookupHTTP(t, h, "POST", base+"/"+id+"/reveal", `{}`, token, "merchant", csrf); w.Code != 409 {
				t.Fatal("repeat reveal", w.Code)
			}
			// Unknown query fields and malformed codes must not reach the real
			// exchange adapter. A valid foreign-app callback burns its state.
			data = create()
			path, state, cookie = start(data)
			unknown := httptest.NewRequest("GET", path+"?state="+state+"&unknown=x", nil)
			unknown.AddCookie(cookie)
			w = httptest.NewRecorder()
			h.ServeHTTP(w, unknown)
			if w.Code < 400 || exchanges.Load() != 1 {
				t.Fatal("unknown callback query reached exchange")
			}
			for _, badCode := range []string{"@bad", "bad code", "bad\x00code"} {
				if w = callback(path, state, badCode, cookie); w.Code < 400 || exchanges.Load() != 1 {
					t.Fatal("malformed code exchanged")
				}
			}
			foreignURL, _ := url.Parse(path)
			foreignQuery := url.Values{"state": {state}, "app_id": {"foreign-app"}}
			if kind == "wxpay" {
				foreignQuery.Set("code", "foreign-app-code")
			} else {
				foreignQuery.Set("auth_code", "foreign-app-code")
			}
			foreignURL.RawQuery = foreignQuery.Encode()
			foreign := httptest.NewRequest("GET", foreignURL.String(), nil)
			foreign.AddCookie(cookie)
			w = httptest.NewRecorder()
			h.ServeHTTP(w, foreign)
			if w.Code < 400 || exchanges.Load() != 1 {
				t.Fatal("foreign app exchanged")
			}
			if w = callback(path, state, "fresh-code", cookie); w.Code < 400 || exchanges.Load() != 1 {
				t.Fatal("foreign-app callback revived state")
			}
			data = create()
			path, state, cookie = start(data)
			s.Sandbox = true
			if w = callback(path, state, "disabled-code", cookie); w.Code < 400 || exchanges.Load() != 1 {
				t.Fatal("disabled lookup exchanged")
			}
			s.Sandbox = false
			if w = callback(path, state, "disabled-code", cookie); w.Code < 400 || exchanges.Load() != 1 {
				t.Fatal("disabled callback state revived")
			}
			data = create()
			path, state, cookie = start(data)
			fail.Store(true)
			if w = callback(path, state, "failed-code", cookie); w.Code < 400 {
				t.Fatal("exchange failure accepted")
			}
			fail.Store(false)
			failedStatus := lookupHTTP(t, h, "GET", base+"/"+data["request_id"].(string), "", token, "merchant", csrf)
			if failedStatus.Code != 200 || lookupData(t, failedStatus)["status"] != "failed" {
				t.Fatal("exchange failure not terminal", failedStatus.Body.String())
			}
			if w = callback(path, state, "failed-code", cookie); w.Code < 400 || exchanges.Load() != 2 {
				t.Fatal("failed code retried")
			}
			data = create()
			path, state, cookie = start(data)
			if kind == "wxpay" {
				changed, err := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "lookup-app", MerchantID: "changed-mch-same-app", APIKey: "0123456789abcdef0123456789abcdef", Methods: []string{"jsapi"}})
				if err != nil {
					t.Fatal(err)
				}
				s.ProviderFlow.Providers[7] = changed
				if w = callback(path, state, "account-changed-code", cookie); w.Code < 400 || exchanges.Load() != 2 {
					t.Fatal("same AppID different account exchanged", w.Code)
				}
				s.ProviderFlow.Providers[7] = p
				data = create()
				path, state, cookie = start(data)
			}
			onExchange = func() {
				if _, err := pool.Exec(context.Background(), `DELETE FROM console_sessions`); err != nil {
					t.Error(err)
				}
			}
			if w = callback(path, state, "revoked-code", cookie); w.Code < 400 {
				t.Fatal("slow exchange outlived session")
			}
			onExchange = nil
			for _, table := range []string{"orders", "provider_attempts", "journals"} {
				var n int
				if e = pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); e != nil || n != 0 {
					t.Fatal("identity lookup touched payment", table, n, e)
				}
			}
			var raw string
			if e = pool.QueryRow(context.Background(), `SELECT jsonb_agg(to_jsonb(q))::text FROM identity_lookup_requests q`).Scan(&raw); e != nil || strings.Contains(raw, "private-lookup-openid") || strings.Contains(raw, token) || strings.Contains(raw, "one-code") {
				t.Fatal("plaintext persisted", e)
			}
		})
	}
}
