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
	"goepay/internal/channelconfig"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdentityProfile64HTTPRealTLSIndependentAppAndFinancialWritesOff(t *testing.T) {
	for _, kind := range []string{"web", "mini"} {
		for _, wallet := range []string{"wxpay", "alipay"} {
			t.Run(wallet+"_"+kind, func(t *testing.T) {
				s, p, key := fixture(t)
				s.Sandbox = false
				s.Sessions = identity.NewPersistentSessions(p, true)
				token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
				if e != nil {
					t.Fatal(e)
				}
				platform, e := rsa.GenerateKey(rand.Reader, 2048)
				if e != nil {
					t.Fatal(e)
				}
				var exchanges atomic.Int32
				up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					exchanges.Add(1)
					if wallet == "wxpay" {
						want := "/sns/oauth2/access_token"
						if kind == "mini" {
							want = "/sns/jscode2session"
						}
						if r.URL.Path != want || r.URL.Query().Get("appid") != "independent-app" || r.URL.Query().Get("secret") != "synthetic-secret" {
							t.Error("not independent fixed OAuth API", r.URL.Path)
							w.WriteHeader(500)
							return
						}
						out := map[string]any{"openid": "private-profile-openid", "session_key": "discard-mini-key", "access_token": "discard-access", "scope": "snsapi_base", "expires_in": 7200}
						json.NewEncoder(w).Encode(out)
						return
					}
					r.ParseForm()
					if r.Form.Get("method") != "alipay.system.oauth.token" || r.Form.Get("app_id") != "independent-app" || r.Form.Get("app_auth_token") != "" || r.Form.Get("biz_content") != "" {
						t.Error("wrong Alipay identity method/context", r.Form.Get("method"))
						w.WriteHeader(500)
						return
					}
					node, _ := json.Marshal(map[string]any{"open_id": "private-profile-openid", "access_token": "discard-access", "expires_in": "3600"})
					digest := sha256.Sum256(node)
					sig, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
					json.NewEncoder(w).Encode(map[string]any{"alipay_system_oauth_token_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(sig)})
				}))
				defer up.Close()
				profile := channelconfig.IdentityProfile{ID: 11, Revision: 1, ChannelID: 7, Provider: wallet, Kind: kind, AppID: "independent-app", MerchantIDs: []int64{1}, Fingerprint: strings.Repeat("f", 64)}
				if kind == "mini" {
					profile.Path = "pages/identity/index"
				}
				var payer adapter.Provider
				if wallet == "wxpay" {
					client := &http.Client{Transport: oauthWechatTLSTransport{up, t}}
					payer, e = adapter.NewWechatV2(adapter.WechatV2Config{AppID: "payment-app", MerchantID: "payment-mch", APIKey: strings.Repeat("k", 32), Methods: []string{"native"}, HTTPClient: client})
					if kind == "web" {
						profile.Web, e = adapter.NewWechatOAuth(adapter.WechatOAuthConfig{AppID: profile.AppID, AppSecret: "synthetic-secret", HTTPClient: client})
					} else {
						profile.Mini, e = adapter.NewWechatMiniOAuth(adapter.WechatOAuthConfig{AppID: profile.AppID, AppSecret: "synthetic-secret", HTTPClient: client})
					}
				} else {
					client := &http.Client{Transport: oauthContractTLSTransport{up, t}}
					payer, e = adapter.NewAlipay(adapter.AlipayConfig{AppID: "payment-app", PrivateKey: key, PublicKey: &platform.PublicKey, Methods: []string{"native"}, HTTPClient: client})
					var ali *adapter.Alipay
					ali, e = adapter.NewAlipay(adapter.AlipayConfig{AppID: profile.AppID, PrivateKey: key, PublicKey: &platform.PublicKey, Methods: []string{"jsapi"}, HTTPClient: client})
					if kind == "web" {
						profile.Web = ali
					} else {
						profile.Mini = ali
					}
				}
				if e != nil {
					t.Fatal(e)
				}
				s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: payer}, AllowLive: false, CallbackBaseURL: "https://pay.example.invalid"}
				s.IdentityProfileCallbackBaseURL = "https://pay.example.invalid"
				s.MerchantChannels = map[int64][]int64{1: {7}}
				s.IdentityProfiles = map[int64]channelconfig.IdentityProfile{11: profile}
				h := s.Handler()
				base := "/console/v1/merchant/identity-profile-lookups"
				cap := lookupHTTP(t, h, "GET", base+"/capabilities", "", token, "merchant", csrf)
				if cap.Code != 200 || lookupData(t, cap)["available"] != false {
					t.Fatal("default not disabled", cap.Code, cap.Body.String())
				}
				s.IdentityProfileEnabled = true
				cap = lookupHTTP(t, h, "GET", base+"/capabilities", "", token, "merchant", csrf)
				if cap.Code != 200 || lookupData(t, cap)["available"] != true || exchanges.Load() != 0 {
					t.Fatal("independent profile tied to payment writes/products", cap.Code, cap.Body.String())
				}
				body := `{"profile_id":"11","profile_revision":"1"}`
				for _, invalid := range []string{`{"profile_id":11,"profile_revision":"1"}`, `{"profile_id":"11","profile_revision":"1","merchant_id":null}`, `{"profile_id":"11","profile_revision":"1","profile_id":"11"}`, `{"profile_id":"11","profile_revision":"1","subject_id":"injected"}`} {
					if w := lookupHTTP(t, h, "POST", base, invalid, token, "merchant", csrf); w.Code != 400 {
						t.Fatal("ambiguous request accepted", w.Code)
					}
				}
				if w := lookupHTTP(t, h, "GET", base+"/capabilities?merchant_id=1&merchant_id=1", "", token, "merchant", csrf); w.Code != 400 {
					t.Fatal("duplicate capability query", w.Code)
				}
				if w := lookupHTTP(t, h, "POST", base, body, token, "merchant", ""); w.Code != 403 {
					t.Fatal("CSRF", w.Code)
				}
				created := lookupHTTP(t, h, "POST", base, body, token, "merchant", csrf)
				if created.Code != 201 {
					t.Fatal(created.Code, created.Body.String())
				}
				data := lookupData(t, created)
				request := data["request"].(map[string]any)
				id := request["request_id"].(string)
				other, otherCSRF, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
				if e != nil {
					t.Fatal(e)
				}
				if w := lookupHTTP(t, h, "GET", base+"/"+id, "", other, "merchant", otherCSRF); w.Code != 403 {
					t.Fatal("different original session read", w.Code)
				}
				if w := lookupHTTP(t, h, "GET", base+"/"+id+"?state=not-accepted", "", token, "merchant", csrf); w.Code != 400 {
					t.Fatal("status query accepted", w.Code)
				}
				if strings.Contains(created.Body.String(), profile.Fingerprint) || strings.Contains(created.Body.String(), "discard-") || exchanges.Load() != 0 {
					t.Fatal("creation leaked or exchanged")
				}
				var browser *http.Cookie
				var state string
				var callback string
				if kind == "mini" {
					launch := data["launch"].(map[string]any)
					if data["url_link_available"] != false || launch["kind"] != "sdk" || launch["app_id"] != profile.AppID || strings.Contains(created.Body.String(), "money") {
						t.Fatal("fake payment launch", data)
					}
					state = launch["body"].(map[string]any)["state"].(string)
					callback = launch["completion_url"].(string)
					u, _ := url.Parse(callback)
					bad := lookupHTTP(t, h, "POST", u.RequestURI(), `{"state":"`+state+`","code":"local-profile-code","openid":"forged"}`, "", "", "")
					if bad.Code != 400 || exchanges.Load() != 0 {
						t.Fatal("injected subject accepted", bad.Code)
					}
					completion, owner, role, proof := u.RequestURI(), "", "", ""
					if wallet == "alipay" {
						completion, owner, role, proof = base+"/"+id+"/complete", token, "merchant", csrf
					}
					done := lookupHTTP(t, h, "POST", completion, `{"state":"`+state+`","code":"local-profile-code"}`, owner, role, proof)
					if done.Code != 200 {
						t.Fatal(done.Code, done.Body.String())
					}
					replay := lookupHTTP(t, h, "POST", u.RequestURI(), `{"state":"`+state+`","code":"local-profile-code"}`, "", "", "")
					if replay.Code != 409 || exchanges.Load() != 1 {
						t.Fatal("mini replay", replay.Code, exchanges.Load())
					}
				} else {
					start := lookupHTTP(t, h, "GET", data["authorization_url"].(string), "", "", "", "")
					if start.Code != 302 {
						t.Fatal(start.Code, start.Body.String())
					}
					browser = start.Result().Cookies()[0]
					u, _ := url.Parse(start.Header().Get("Location"))
					state = u.Query().Get("state")
					callback = u.Query().Get("redirect_uri")
					v := url.Values{"state": {state}, "app_id": {profile.AppID}}
					if wallet == "wxpay" {
						v.Set("code", "local-profile-code")
					} else {
						v.Set("auth_code", "local-profile-code")
					}
					r := httptest.NewRequest("GET", callback+"?"+v.Encode(), nil)
					r.AddCookie(browser)
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					if w.Code != 200 || strings.Contains(w.Body.String(), "private-profile-openid") {
						t.Fatal("web callback", w.Code, w.Body.String())
					}
				}
				status := lookupHTTP(t, h, "GET", base+"/"+id, "", token, "merchant", csrf)
				if status.Code != 200 || lookupData(t, status)["status"] != "verified" || strings.Contains(status.Body.String(), "private-profile-openid") {
					t.Fatal("status leak/failure", status.Code, status.Body.String())
				}
				revealed := lookupHTTP(t, h, "POST", base+"/"+id+"/reveal", `{}`, token, "merchant", csrf)
				if revealed.Code != 200 || lookupData(t, revealed)["subject_id"] != "private-profile-openid" {
					t.Fatal("reveal", revealed.Code, revealed.Body.String())
				}
				again := lookupHTTP(t, h, "POST", base+"/"+id+"/reveal", `{}`, token, "merchant", csrf)
				if again.Code != 409 || exchanges.Load() != 1 {
					t.Fatal("duplicate reveal/exchange", again.Code, exchanges.Load())
				}
				for _, table := range []string{"orders", "provider_attempts", "ledger_entries", "jobs"} {
					var n int
					if e = p.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); e != nil || n != 0 {
						t.Fatal("identity changed money", table, n, e)
					}
				}
			})
		}
	}
}

func TestIdentityProfile64HTTPRevocationDuringLocalTLSExchangeCannotReveal(t *testing.T) {
	s, p, _ := fixture(t)
	s.Sandbox = false
	s.Sessions = identity.NewPersistentSessions(p, true)
	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var exchanges atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		close(entered)
		<-release
		json.NewEncoder(w).Encode(map[string]any{"openid": "private-revoked-subject", "session_key": "discard"})
	}))
	defer up.Close()
	client := &http.Client{Transport: oauthWechatTLSTransport{up, t}}
	mini, e := adapter.NewWechatMiniOAuth(adapter.WechatOAuthConfig{AppID: "independent-app", AppSecret: "synthetic-secret", HTTPClient: client})
	if e != nil {
		t.Fatal(e)
	}
	s.IdentityProfileEnabled = true
	s.IdentityProfileCallbackBaseURL = "https://pay.example.invalid"
	s.MerchantChannels = map[int64][]int64{1: {7}}
	s.IdentityProfiles = map[int64]channelconfig.IdentityProfile{11: {ID: 11, Revision: 1, ChannelID: 7, Provider: "wxpay", Kind: "mini", AppID: "independent-app", Path: "pages/identity/index", MerchantIDs: []int64{1}, Fingerprint: strings.Repeat("f", 64), Mini: mini}}
	h := s.Handler()
	base := "/console/v1/merchant/identity-profile-lookups"
	created := lookupHTTP(t, h, "POST", base, `{"profile_id":"11","profile_revision":"1"}`, token, "merchant", csrf)
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	data := lookupData(t, created)
	q := data["request"].(map[string]any)
	id := q["request_id"].(string)
	launch := data["launch"].(map[string]any)
	state := launch["body"].(map[string]any)["state"].(string)
	u, _ := url.Parse(launch["completion_url"].(string))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- lookupHTTP(t, h, "POST", u.RequestURI(), `{"state":"`+state+`","code":"revocation-code"}`, "", "", "")
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("local TLS request missing")
	}
	if _, e = p.Exec(context.Background(), `UPDATE console_users SET auth_version=auth_version+1 WHERE username='merchant'`); e != nil {
		close(release)
		t.Fatal(e)
	}
	close(release)
	w := <-done
	if w.Code != 409 || strings.Contains(w.Body.String(), "private-revoked-subject") {
		t.Fatal("revoked exchange accepted", w.Code)
	}
	var status string
	var cipher []byte
	if e = p.QueryRow(context.Background(), `SELECT status,subject_cipher FROM profile_identity_lookup_requests WHERE id=$1`, id).Scan(&status, &cipher); e != nil || status != "failed" || cipher != nil {
		t.Fatal("revoked exchange retained result", status, e)
	}
	if revealed := lookupHTTP(t, h, "POST", base+"/"+id+"/reveal", `{}`, token, "merchant", csrf); revealed.Code == 200 {
		t.Fatal("revoked result revealed")
	}
	replay := lookupHTTP(t, h, "POST", u.RequestURI(), `{"state":"`+state+`","code":"revocation-code"}`, "", "", "")
	if replay.Code == 200 || exchanges.Load() != 1 {
		t.Fatal("revoked state exchanged again", replay.Code, exchanges.Load())
	}
	for _, table := range []string{"orders", "provider_attempts", "ledger_entries", "jobs"} {
		var n int
		if e = p.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("financial side effect", table, n, e)
		}
	}
}
