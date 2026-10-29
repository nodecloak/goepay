package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestConnectLoginRelayProviders covers WeChat (c2) and Alipay (c3), which share
// the self-hosted Oauth relay (includes/lib/Oauth.php). Each: off by default,
// authenticated bind links social_uid -> merchant, login logs in the bound
// merchant, and an unknown social_uid is refused and never logged in.
func TestConnectLoginRelayProviders(t *testing.T) {
	for _, provider := range []string{"wx", "alipay"} {
		t.Run(provider, func(t *testing.T) {
			f := email197Local(t)
			const mid int64 = 1
			socialUID := "RELAY-" + provider + "-UID"
			relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/connect.php") {
					w.WriteHeader(404)
					return
				}
				switch r.URL.Query().Get("act") {
				case "login":
					w.Write([]byte(`{"code":0,"url":"https://open.example.invalid/authorize?state=` + r.URL.Query().Get("state") + `"}`))
				case "callback":
					if r.URL.Query().Get("code") == "good" {
						w.Write([]byte(`{"code":0,"social_uid":"` + socialUID + `","access_token":"AT"}`))
					} else {
						w.Write([]byte(`{"code":-1,"msg":"bad"}`))
					}
				}
			}))
			defer relay.Close()
			f.s.ConnectTokenBase = map[string]string{provider: relay.URL}

			// off by default.
			if w := connectReq(f, "POST", "/console/v1/merchant/account/bindings/connect/"+provider+"/start", true); w.Code != 503 {
				t.Fatal("bind-start must be 503 while off", w.Code)
			}
			enableKey := "login_" + provider
			seedConnectSetting(t, f, enableKey, "-1")
			seedConnectSetting(t, f, "login_appid", "relayapp")
			seedConnectSetting(t, f, "login_appkey", "relaysecret")
			seedConnectSetting(t, f, "login_apiurl", relay.URL)

			// bind start -> authorize url + cookie.
			w := connectReq(f, "POST", "/console/v1/merchant/account/bindings/connect/"+provider+"/start", true)
			if w.Code != 200 {
				t.Fatal("bind-start", w.Code, w.Body.String())
			}
			au := connectAuthorizeURL(t, w)
			state := au.Query().Get("state")
			nonce := connectCookie(w, connectLoginBrowserCookie)
			if state == "" || nonce == "" {
				t.Fatal("bind-start missing state/nonce")
			}
			cb := connectCallback(f, provider, state, nonce, "good")
			if cb.Code != 302 {
				t.Fatal("bind callback", cb.Code, cb.Body.String())
			}
			var bound string
			if e := f.p.QueryRow(t.Context(), `SELECT profile->>($1||'_uid')FROM merchants WHERE id=$2`, provider, mid).Scan(&bound); e != nil || bound != socialUID {
				t.Fatal("social_uid not linked", bound, e)
			}

			// login with the bound uid -> session.
			ls := connectLoginStart(f, provider)
			lstate := loginStateFromRedirect(t, ls)
			lnonce := connectCookie(ls, connectLoginBrowserCookie)
			lc := connectCallback(f, provider, lstate, lnonce, "good")
			if lc.Code != 302 || connectCookie(lc, "goepay_merchant") == "" {
				t.Fatal("bound login should set a session", lc.Code)
			}

			// unknown uid -> refused, no cookie.
			relay2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("act") == "login" {
					w.Write([]byte(`{"code":0,"url":"https://open.example.invalid/authorize?state=` + r.URL.Query().Get("state") + `"}`))
				} else {
					w.Write([]byte(`{"code":0,"social_uid":"UNKNOWN-RELAY-UID","access_token":"AT"}`))
				}
			}))
			defer relay2.Close()
			f.s.ConnectTokenBase = map[string]string{provider: relay2.URL}
			us := connectLoginStart(f, provider)
			ustate := loginStateFromRedirect(t, us)
			unonce := connectCookie(us, connectLoginBrowserCookie)
			uc := connectCallback(f, provider, ustate, unonce, "good")
			// Unknown identity is NOT logged in: the connectstate135 pending-connect
			// flow records a single-use pending state and redirects to the login page
			// carrying ?connect=<pending> (see connect_login_complete_test.go). Never
			// a session cookie.
			if uc.Code != 302 {
				t.Fatal("unknown uid must land on the login page, never logged in", uc.Code, uc.Body.String())
			}
			uloc, _ := url.Parse(uc.Header().Get("Location"))
			if !strings.HasSuffix(uloc.Path, "/merchant/login") || uloc.Query().Get("connect") == "" {
				t.Fatal("unknown uid must carry a pending connect state", uloc.String())
			}
			if connectCookie(uc, "goepay_merchant") != "" {
				t.Fatal("unknown uid must not set a session cookie")
			}
		})
	}
}

func connectAuthorizeURL(t *testing.T, w *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	var out struct {
		Data struct {
			AuthorizeURL string `json:"authorize_url"`
		}
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal("authorize url parse", e)
	}
	u, e := url.Parse(out.Data.AuthorizeURL)
	if e != nil {
		t.Fatal("authorize url", e)
	}
	return u
}
