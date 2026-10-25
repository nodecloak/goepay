package contract_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestApplicationAuthorizationHTTPIndependentTLSAndSingleReveal(t *testing.T) {
	for _, scenario := range []struct {
		kind, mode string
		wallet     bool
	}{{"basic", "success", false}, {"assigned", "success", false}, {"assigned", "success", true}, {"basic", "revoked", false}, {"basic", "unknown", false}} {
		name := scenario.kind + "_" + scenario.mode
		if scenario.kind == "assigned" {
			if scenario.wallet {
				name += "_wallet"
			} else {
				name += "_browser"
			}
		}
		t.Run(name, func(t *testing.T) {
			kind := scenario.kind
			s, p, key := fixture(t)
			s.Sandbox = false
			s.Sessions = identity.NewPersistentSessions(p, true)
			token, csrf, err := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"})
			if err != nil {
				t.Fatal(err)
			}
			platform, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				var biz map[string]string
				if r.URL.Path != "/gateway.do" || r.Form.Get("method") != "alipay.open.auth.token.app" || r.Form.Get("app_id") != "2021000000000001" || json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz) != nil || biz["grant_type"] != "authorization_code" || biz["code"] != "synthetic-app-auth-code" || r.Form.Get("app_auth_token") != "" {
					t.Error("wrong application authorization exchange")
					w.WriteHeader(500)
					return
				}
				var processing bool
				if e := p.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM application_authorization_requests WHERE status='processing') AND EXISTS(SELECT 1 FROM application_authorization_events WHERE operation='processing')`).Scan(&processing); e != nil || !processing {
					t.Error("exchange preceded durable claim", e)
					w.WriteHeader(500)
					return
				}
				if scenario.mode == "unknown" {
					w.WriteHeader(503)
					w.Write([]byte("private-provider-error-must-not-escape"))
					return
				}
				if scenario.mode == "revoked" {
					if _, err := p.Exec(context.Background(), `UPDATE console_users SET auth_version=auth_version+1 WHERE username='admin'`); err != nil {
						t.Error(err)
					}
				}
				node, _ := json.Marshal(map[string]any{"code": "10000", "auth_app_id": "2021000000000002", "user_id": "2088000000000001", "app_auth_token": "private-app-authorization-token", "app_refresh_token": "discard-private-refresh", "expires_in": "3600"})
				digest := sha256.Sum256(node)
				sig, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
				json.NewEncoder(w).Encode(map[string]any{"alipay_open_auth_token_app_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(sig)})
			}))
			defer up.Close()
			client, err := adapter.NewAlipayApplicationAuthorization(adapter.AlipayConfig{AppID: "2021000000000001", PrivateKey: key, PublicKey: &platform.PublicKey, HTTPClient: &http.Client{Transport: oauthContractTLSTransport{up, t}}})
			if err != nil {
				t.Fatal(err)
			}
			profile := channelconfig.ApplicationAuthorizationProfile{ID: 21, Revision: 1, IssuerAppID: "2021000000000001", ExpectedAuthAppID: "2021000000000002", LaunchKind: kind, MerchantIDs: []int64{1}, Fingerprint: strings.Repeat("a", 64), Adapter: client}
			if kind == "assigned" {
				profile.AppTypes = []adapter.AlipayAuthorizationAppType{adapter.AlipayAuthorizationWebApp}
			}
			s.ApplicationAuthorizationProfiles = map[int64]channelconfig.ApplicationAuthorizationProfile{21: profile}
			s.ApplicationAuthorizationCallbackBaseURL = "https://pay.example.invalid"
			s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, AllowLive: false, CallbackBaseURL: "https://pay.example.invalid"}
			h := s.Handler()
			base := "/console/v1/admin/application-authorizations"
			cap := lookupHTTP(t, h, "GET", base+"/capabilities?merchant_id=1", "", token, "admin", csrf)
			if cap.Code != 200 || lookupData(t, cap)["available"] != false || calls.Load() != 0 {
				t.Fatal("default must be off", cap.Code, cap.Body.String())
			}
			s.ApplicationAuthorizationEnabled = true
			cap = lookupHTTP(t, h, "GET", base+"/capabilities?merchant_id=1", "", token, "admin", csrf)
			if cap.Code != 200 || lookupData(t, cap)["available"] != true || calls.Load() != 0 {
				t.Fatal("explicit authorization independent of writes", cap.Code, cap.Body.String())
			}
			if lookupData(t, cap)["authorization_origin"] != "https://pay.example.invalid" {
				t.Fatal("trusted public origin missing", cap.Body.String())
			}
			for _, invalid := range []string{`{"profile_id":21,"profile_revision":"1","merchant_id":"1"}`, `{"profile_id":"21","profile_revision":"1","merchant_id":null}`, `{"profile_id":"21","profile_id":"21","profile_revision":"1","merchant_id":"1"}`, `{"profile_id":"21","profile_revision":"1","merchant_id":"1","app_auth_token":"injected"}`} {
				w := lookupHTTP(t, h, "POST", base, invalid, token, "admin", csrf)
				if w.Code != 400 {
					t.Fatal("ambiguous creation accepted", w.Code)
				}
			}
			created := lookupHTTP(t, h, "POST", base, `{"profile_id":"21","profile_revision":"1","merchant_id":"1"}`, token, "admin", csrf)
			if created.Code != 201 {
				t.Fatal(created.Code, created.Body.String())
			}
			data := lookupData(t, created)
			request := data["request"].(map[string]any)
			id := request["request_id"].(string)
			if request["purpose"] != "alipay_app_authorization" || calls.Load() != 0 {
				t.Fatal("mixed purpose or premature exchange", data)
			}
			startPath := data["authorization_url"].(string)
			if scenario.wallet {
				startPath = data["wallet_authorization_url"].(string)
			}
			start := lookupHTTP(t, h, "GET", startPath, "", "", "", "")
			if start.Code != 302 || len(start.Result().Cookies()) != 1 {
				t.Fatal("authorization start", start.Code, start.Body.String())
			}
			browser := start.Result().Cookies()[0]
			if !browser.Secure || !browser.HttpOnly || browser.SameSite != http.SameSiteLaxMode || browser.Name != "__Host-goepay_application_authorization" {
				t.Fatal("unbound browser cookie", browser)
			}
			launch, _ := url.Parse(start.Header().Get("Location"))
			state, callback := launch.Query().Get("state"), launch.Query().Get("redirect_uri")
			if kind == "assigned" {
				encoded := launch.Query().Get("bizData")
				if scenario.wallet {
					page := launch.Query().Get("page")
					var ok bool
					_, encoded, ok = strings.Cut(page, "?bizData=")
					if !ok {
						t.Fatal("assigned wallet page", page)
					}
				}
				var biz struct {
					Agent struct {
						State    string `json:"state"`
						Callback string `json:"redirectUri"`
					} `json:"agentOpParam"`
				}
				if json.Unmarshal([]byte(encoded), &biz) != nil {
					t.Fatal("assigned data", encoded)
				}
				state, callback = biz.Agent.State, biz.Agent.Callback
			}
			if state == "" || callback == "" {
				t.Fatal("missing independent launch binding")
			}
			invoke := func(values url.Values) *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", callback+"?"+values.Encode(), nil)
				r.AddCookie(browser)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}
			bad := invoke(url.Values{"state": {state}, "app_id": {profile.IssuerAppID}, "auth_code": {"not-app-code"}})
			if bad.Code != 400 || calls.Load() != 0 {
				t.Fatal("ordinary OAuth code accepted", bad.Code, calls.Load())
			}
			bad = invoke(url.Values{"state": {state}, "app_id": {"2021000000000099"}, "app_auth_code": {"synthetic-app-auth-code"}})
			if bad.Code != 400 || calls.Load() != 0 {
				t.Fatal("wrong issuer reached exchange", bad.Code, calls.Load())
			}
			done := invoke(url.Values{"state": {state}, "app_id": {profile.IssuerAppID}, "app_auth_code": {"synthetic-app-auth-code"}})
			if scenario.mode != "success" {
				if done.Code < 400 || calls.Load() != 1 || strings.Contains(done.Body.String(), "private-") {
					t.Fatal("revoked/unknown callback accepted or leaked", done.Code, done.Body.String(), calls.Load())
				}
				for i := 0; i < 3; i++ {
					again := invoke(url.Values{"state": {state}, "app_id": {profile.IssuerAppID}, "app_auth_code": {"synthetic-app-auth-code"}})
					if again.Code < 400 || calls.Load() != 1 {
						t.Fatal("failed exchange rearmed", again.Code, calls.Load())
					}
				}
				reveal := lookupHTTP(t, h, "POST", base+"/"+id+"/reveal", `{}`, token, "admin", csrf)
				if reveal.Code < 400 || strings.Contains(reveal.Body.String(), "private-") {
					t.Fatal("failed original authority revealed", reveal.Code, reveal.Body.String())
				}
				var verified, finances int
				if err := p.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM application_authorization_requests WHERE status IN ('verified','revealed')),(SELECT count(*) FROM ledger_entries)+(SELECT count(*) FROM jobs)+(SELECT count(*) FROM orders)`).Scan(&verified, &finances); err != nil || verified != 0 || finances != 0 {
					t.Fatal("failed callback persisted or touched money", verified, finances, err)
				}
				return
			}
			if done.Code != 200 || calls.Load() != 1 || strings.Contains(done.Body.String(), "private-") {
				t.Fatal("callback leaked or failed", done.Code, done.Body.String(), calls.Load())
			}
			replay := invoke(url.Values{"state": {state}, "app_id": {profile.IssuerAppID}, "app_auth_code": {"synthetic-app-auth-code"}})
			if replay.Code != 409 || calls.Load() != 1 {
				t.Fatal("replayed external exchange", replay.Code, calls.Load())
			}
			status := lookupHTTP(t, h, "GET", base+"/"+id, "", token, "admin", csrf)
			if status.Code != 200 || lookupData(t, status)["status"] != "verified" || strings.Contains(status.Body.String(), "private-") {
				t.Fatal("status leaked or failed", status.Code, status.Body.String())
			}
			other, otherCSRF, err := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"})
			if err != nil {
				t.Fatal(err)
			}
			if w := lookupHTTP(t, h, "POST", base+"/"+id+"/reveal", `{}`, other, "admin", otherCSRF); w.Code != 403 {
				t.Fatal("different super session revealed", w.Code)
			}
			reveal := lookupHTTP(t, h, "POST", base+"/"+id+"/reveal", `{}`, token, "admin", csrf)
			if reveal.Code != 200 || lookupData(t, reveal)["app_auth_token"] != "private-app-authorization-token" || strings.Contains(reveal.Body.String(), "discard-private-refresh") {
				t.Fatal("typed one-shot reveal", reveal.Code, reveal.Body.String())
			}
			if w := lookupHTTP(t, h, "POST", base+"/"+id+"/reveal", `{}`, token, "admin", csrf); w.Code != 409 {
				t.Fatal("repeat reveal", w.Code)
			}
			for _, table := range []string{"orders", "provider_attempts", "ledger_entries", "jobs"} {
				var n int
				if err := p.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
					t.Fatal("authorization changed money", table, n, err)
				}
			}
		})
	}
}
