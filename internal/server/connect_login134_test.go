package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func seedConnectSetting(t *testing.T, f *email197Fixture, key, value string) {
	t.Helper()
	if _, e := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body)VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, key, value); e != nil {
		t.Fatal(e)
	}
}

// TestConnectLoginQQEndToEnd exercises the QQ quick-login: authenticated bind
// start+callback links openid->merchant; login callback logs in a BOUND merchant
// and refuses an unknown openid with the original message, never logging in.
func TestConnectLoginQQEndToEnd(t *testing.T) {
	f := email197Local(t)
	const mid int64 = 1

	// A fake QQ token endpoint returning a fixed openid for the code.
	qqOpenID := "QQ-OPENID-END2END"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/authorize") {
			w.WriteHeader(200)
			return
		}
		if r.URL.Query().Get("code") == "good-code" {
			w.Write([]byte(`{"access_token":"AT","openid":"` + qqOpenID + `"}`))
		} else {
			w.Write([]byte(`{"error":"100019","error_description":"bad"}`))
		}
	}))
	defer upstream.Close()
	f.s.ConnectTokenBase = map[string]string{"qq": upstream.URL}

	// Off by default: bind-start is refused until the admin enables QQ.
	if w := connectReq(f, "POST", "/console/v1/merchant/account/bindings/connect/qq/start", true); w.Code != 503 {
		t.Fatal("bind-start must be 503 while QQ is off", w.Code, w.Body.String())
	}
	seedConnectSetting(t, f, "login_qq", "1")
	seedConnectSetting(t, f, "login_qq_appid", "qqapp")
	seedConnectSetting(t, f, "login_qq_appkey", "qqsecret")

	// Authenticated bind start -> authorize URL + browser nonce cookie.
	w := connectReq(f, "POST", "/console/v1/merchant/account/bindings/connect/qq/start", true)
	if w.Code != 200 {
		t.Fatal("bind-start", w.Code, w.Body.String())
	}
	var start struct {
		Code int
		Data struct {
			AuthorizeURL string `json:"authorize_url"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &start)
	u, _ := url.Parse(start.Data.AuthorizeURL)
	bindState := u.Query().Get("state")
	if bindState == "" {
		t.Fatal("authorize url missing state", start.Data.AuthorizeURL)
	}
	browserNonce := connectCookie(w, connectLoginBrowserCookie)
	if browserNonce == "" {
		t.Fatal("bind-start did not set a browser nonce cookie")
	}
	// Missing CSRF is refused.
	if bad := connectReq(f, "POST", "/console/v1/merchant/account/bindings/connect/qq/start", false); bad.Code != 403 {
		t.Fatal("bind-start without CSRF should be 403", bad.Code)
	}

	// Bind callback with a mismatched browser nonce is refused (CSRF defence).
	if cb := connectCallback(f, "qq", bindState, "wrong-nonce", "good-code"); cb.Code != 403 {
		t.Fatal("bind callback with wrong browser nonce should be 403", cb.Code)
	}
	// Bind callback with the right nonce links the openid (302 back to account).
	cb := connectCallback(f, "qq", bindState, browserNonce, "good-code")
	if cb.Code != 302 {
		t.Fatal("bind callback should redirect on success", cb.Code, cb.Body.String())
	}
	var boundID string
	if e := f.p.QueryRow(t.Context(), `SELECT profile->>'qq_uid' FROM merchants WHERE id=$1`, mid).Scan(&boundID); e != nil || boundID != qqOpenID {
		t.Fatal("qq openid not linked to the merchant", boundID, e)
	}
	// The state is single-use: replaying the same callback is refused.
	if replay := connectCallback(f, "qq", bindState, browserNonce, "good-code"); replay.Code != 403 {
		t.Fatal("replayed bind state should be refused", replay.Code)
	}

	// LOGIN: a start issues a login state; its callback logs the bound merchant in.
	ls := connectLoginStart(f, "qq")
	if ls.Code != 302 {
		t.Fatal("login start should redirect to provider", ls.Code)
	}
	loginState := loginStateFromRedirect(t, ls)
	loginNonce := connectCookie(ls, connectLoginBrowserCookie)
	lc := connectCallback(f, "qq", loginState, loginNonce, "good-code")
	if lc.Code != 302 || connectCookie(lc, "goepay_merchant") == "" {
		t.Fatal("bound openid should log in (302 + merchant cookie)", lc.Code)
	}

	// UNKNOWN openid: a fresh login state, callback with a code that yields an
	// unbound openid -> refused with the original message, no session cookie.
	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/authorize") {
			return
		}
		w.Write([]byte(`{"access_token":"AT","openid":"QQ-OPENID-UNKNOWN"}`))
	}))
	defer upstream2.Close()
	f.s.ConnectTokenBase = map[string]string{"qq": upstream2.URL}
	ls2 := connectLoginStart(f, "qq")
	unknownState := loginStateFromRedirect(t, ls2)
	unknownNonce := connectCookie(ls2, connectLoginBrowserCookie)
	uc := connectCallback(f, "qq", unknownState, unknownNonce, "good-code")
	// Unknown identity is NOT logged in: the connectstate135 pending-connect flow
	// records a single-use pending state and redirects to the login page carrying
	// ?connect=<pending> (see connect_login_complete_test.go). Never a session cookie.
	if uc.Code != 302 {
		t.Fatal("unknown openid must land on the login page, never logged in", uc.Code, uc.Body.String())
	}
	uloc, _ := url.Parse(uc.Header().Get("Location"))
	if !strings.HasSuffix(uloc.Path, "/merchant/login") || uloc.Query().Get("connect") == "" {
		t.Fatal("unknown openid must carry a pending connect state", uloc.String())
	}
	if connectCookie(uc, "goepay_merchant") != "" {
		t.Fatal("unknown openid must not receive a session cookie")
	}
}

func connectReq(f *email197Fixture, method, path string, csrf bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if csrf {
		r.Header.Set("X-CSRF-Token", f.csrf)
	}
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func connectLoginStart(f *email197Fixture, provider string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/console/v1/public/connect/"+provider+"/start", nil)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func connectCallback(f *email197Fixture, provider, state, browser, code string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/console/v1/public/connect/"+provider+"/callback?code="+code+"&state="+state, nil)
	if browser != "" {
		r.AddCookie(&http.Cookie{Name: connectLoginBrowserCookie, Value: browser})
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func connectCookie(w *httptest.ResponseRecorder, name string) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == name && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

func loginStateFromRedirect(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	loc := w.Header().Get("Location")
	u, e := url.Parse(loc)
	if e != nil {
		t.Fatal("bad redirect", loc)
	}
	return u.Query().Get("state")
}
