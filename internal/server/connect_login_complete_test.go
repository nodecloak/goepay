package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestConnectLoginCompleteUnknownIdentity drives the unknown-identity landing:
// the login callback redirects to /merchant/login?connect=<state> with a pending
// connect; connect/complete then refuses a wrong key / replayed state and, on a
// correct key, binds the identity once and logs the merchant in.
func TestConnectLoginCompleteUnknownIdentity(t *testing.T) {
	f := email197Local(t)
	const mid int64 = 1
	// channel168Local seeds merchant 1 with APIKey "test-secret"; key login must
	// be allowed by the platform setting.
	const apiKey = "test-secret"
	seedConnectSetting(t, f, "close_keylogin", "0")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/authorize") {
			return
		}
		w.Write([]byte(`{"access_token":"AT","openid":"QQ-UNBOUND-OPENID"}`))
	}))
	defer upstream.Close()
	f.s.ConnectTokenBase = map[string]string{"qq": upstream.URL}
	seedConnectSetting(t, f, "login_qq", "1")
	seedConnectSetting(t, f, "login_qq_appid", "qqapp")
	seedConnectSetting(t, f, "login_qq_appkey", "qqsecret")

	// Login start -> state + nonce; callback with an UNBOUND openid -> 302 to the
	// login page carrying ?connect=<pending state>.
	ls := connectLoginStart(f, "qq")
	state := loginStateFromRedirect(t, ls)
	nonce := connectCookie(ls, connectLoginBrowserCookie)
	cb := connectCallback(f, "qq", state, nonce, "good-code")
	if cb.Code != 302 {
		t.Fatal("unknown identity should redirect to the login page", cb.Code, cb.Body.String())
	}
	loc, _ := url.Parse(cb.Header().Get("Location"))
	if !strings.HasSuffix(loc.Path, "/merchant/login") {
		t.Fatal("unknown identity must land on the login page", loc.String())
	}
	pendingState := loc.Query().Get("connect")
	if pendingState == "" {
		t.Fatal("login page must carry a pending connect state", loc.String())
	}
	// No merchant session was issued on the unknown-identity callback.
	if connectCookie(cb, "goepay_merchant") != "" {
		t.Fatal("unknown identity must not be logged in")
	}

	complete := func(state, key string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"state": state, "merchant_id": "1", "api_key": key, "code": ""})
		r := httptest.NewRequest("POST", "/console/v1/public/connect/complete", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: connectLoginBrowserCookie, Value: nonce})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}

	// Wrong key is refused (and spends the pending state).
	if w := complete(pendingState, "wrong-api-key"); w.Code != 401 {
		t.Fatal("wrong key should be 401", w.Code, w.Body.String())
	}
	// The pending state was single-use: replaying it (even with the right key) is refused.
	if w := complete(pendingState, apiKey); w.Code != 403 {
		t.Fatal("replayed pending state must be refused", w.Code, w.Body.String())
	}
	// Not yet bound (the failed attempts must not have linked anything).
	var bound string
	_ = f.p.QueryRow(t.Context(), `SELECT COALESCE(profile->>'qq_uid','')FROM merchants WHERE id=$1`, mid).Scan(&bound)
	if bound != "" {
		t.Fatal("no bind should have happened on failed completion", bound)
	}

	// A fresh unknown-identity landing + correct key binds once and logs in.
	ls2 := connectLoginStart(f, "qq")
	state2 := loginStateFromRedirect(t, ls2)
	nonce2 := connectCookie(ls2, connectLoginBrowserCookie)
	cb2 := connectCallback(f, "qq", state2, nonce2, "good-code")
	pending2 := func() string {
		l, _ := url.Parse(cb2.Header().Get("Location"))
		return l.Query().Get("connect")
	}()
	body, _ := json.Marshal(map[string]string{"state": pending2, "merchant_id": "1", "api_key": apiKey, "code": ""})
	r := httptest.NewRequest("POST", "/console/v1/public/connect/complete", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: connectLoginBrowserCookie, Value: nonce2})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || connectCookie(w, "goepay_merchant") == "" {
		t.Fatal("correct key should bind + log in", w.Code, w.Body.String())
	}
	if e := f.p.QueryRow(t.Context(), `SELECT COALESCE(profile->>'qq_uid','')FROM merchants WHERE id=$1`, mid).Scan(&bound); e != nil || bound != "QQ-UNBOUND-OPENID" {
		t.Fatal("identity should be bound after successful completion", bound, e)
	}
}

// TestConnectOptionsReportsEnabledProviders checks the login-page options feed.
func TestConnectOptionsReportsEnabledProviders(t *testing.T) {
	f := email197Local(t)
	get := func() map[string]any {
		r := httptest.NewRequest("GET", "/console/v1/public/connect/options", nil)
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		var out struct {
			Data struct {
				Providers map[string]any `json:"providers"`
			}
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		return out.Data.Providers
	}
	p := get()
	if p["qq"] != false || p["wx"] != false || p["alipay"] != false || p["telegram"] != false {
		t.Fatal("all providers off by default", p)
	}
	seedConnectSetting(t, f, "login_qq", "1")
	seedConnectSetting(t, f, "login_qq_appid", "a")
	seedConnectSetting(t, f, "login_qq_appkey", "k")
	if get()["qq"] != true {
		t.Fatal("qq should report enabled once configured")
	}
}
