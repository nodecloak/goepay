package connectlogin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOauthRelayAuthorizeAndExchange(t *testing.T) {
	var lastAct, lastType, lastState, lastCode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/connect.php") {
			w.WriteHeader(404)
			return
		}
		q := r.URL.Query()
		lastAct = q.Get("act")
		switch lastAct {
		case "login":
			lastType = q.Get("type")
			lastState = q.Get("state")
			w.Write([]byte(`{"code":0,"url":"https://open.example.invalid/authorize?state=` + q.Get("state") + `"}`))
		case "callback":
			lastCode = q.Get("code")
			if q.Get("code") == "good" {
				w.Write([]byte(`{"code":0,"social_uid":"WX-UID-1","access_token":"AT"}`))
			} else {
				w.Write([]byte(`{"code":-1,"msg":"bad code"}`))
			}
		}
	}))
	defer srv.Close()
	o := OauthRelay{APIBase: srv.URL + "/", AppID: "a", AppKey: "k", Type: "wx", CallbackURL: "https://pay.example.invalid/cb"}

	u, e := o.AuthorizeURL(context.Background(), "state-1")
	if e != nil || !strings.Contains(u, "state=state-1") {
		t.Fatal("authorize", u, e)
	}
	if lastAct != "login" || lastType != "wx" || lastState != "state-1" {
		t.Fatal("login request malformed", lastAct, lastType, lastState)
	}
	uid, e := o.Exchange(context.Background(), "good")
	if e != nil || uid != "WX-UID-1" {
		t.Fatal("exchange", uid, e)
	}
	if lastCode != "good" {
		t.Fatal("callback code not sent", lastCode)
	}
	// Fail closed on a non-zero relay code.
	if _, e := o.Exchange(context.Background(), "bad"); e != ErrUpstream {
		t.Fatal("bad code should be ErrUpstream", e)
	}
	// Unconfigured / empty fail closed.
	if _, e := (OauthRelay{}).AuthorizeURL(context.Background(), "s"); e != ErrConfig {
		t.Fatal("unconfigured authorize", e)
	}
	if _, e := o.Exchange(context.Background(), ""); e != ErrUpstream {
		t.Fatal("empty code", e)
	}

	// A relay returning code:0 but no social_uid fails closed (no identity).
	noUID := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":0,"access_token":"AT"}`))
	}))
	defer noUID.Close()
	o2 := OauthRelay{APIBase: noUID.URL, AppID: "a", AppKey: "k", Type: "alipay", CallbackURL: "https://pay.example.invalid/cb"}
	if _, e := o2.Exchange(context.Background(), "x"); e != ErrUpstream {
		t.Fatal("missing social_uid must be ErrUpstream", e)
	}
	// A 500 fails closed.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer bad.Close()
	o3 := OauthRelay{APIBase: bad.URL, AppID: "a", AppKey: "k", Type: "alipay", CallbackURL: "https://pay.example.invalid/cb"}
	if _, e := o3.AuthorizeURL(context.Background(), "s"); e != ErrUpstream {
		t.Fatal("500 authorize must be ErrUpstream", e)
	}
}
