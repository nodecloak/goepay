package connectlogin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestQQAuthorizeURLCarriesState(t *testing.T) {
	q := QQ{AppID: "app1", AppKey: "k", CallbackURL: "https://pay.example.invalid/connect/qq"}
	u, e := q.AuthorizeURL(context.Background(), "state-xyz")
	if e != nil {
		t.Fatal(e)
	}
	parsed, _ := url.Parse(u)
	if parsed.Query().Get("state") != "state-xyz" ||
		parsed.Query().Get("client_id") != "app1" ||
		parsed.Query().Get("response_type") != "code" ||
		parsed.Query().Get("redirect_uri") != "https://pay.example.invalid/connect/qq" {
		t.Fatal("authorize url missing required params", u)
	}
	if _, e := (QQ{}).AuthorizeURL(context.Background(), "s"); e != ErrConfig {
		t.Fatal("unconfigured authorize should be ErrConfig", e)
	}
	if _, e := q.AuthorizeURL(context.Background(), ""); e != ErrConfig {
		t.Fatal("empty state should be refused", e)
	}
}

func TestQQExchangeReturnsOpenIDAndFailsClosed(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(`{"access_token":"AT","openid":"QQ-OPENID-123","expires_in":7776000}`))
	}))
	defer srv.Close()
	q := QQ{AppID: "app1", AppKey: "secret", CallbackURL: "https://pay.example.invalid/connect/qq", TokenBase: srv.URL}
	openid, e := q.Exchange(context.Background(), "auth-code-1")
	if e != nil || openid != "QQ-OPENID-123" {
		t.Fatal("exchange should return openid", openid, e)
	}
	if gotQuery.Get("code") != "auth-code-1" || gotQuery.Get("need_openid") != "1" ||
		gotQuery.Get("grant_type") != "authorization_code" || gotQuery.Get("fmt") != "json" {
		t.Fatal("token request malformed", gotQuery)
	}
	if strings.Contains(gotQuery.Get("client_secret"), "wrong") || gotQuery.Get("client_secret") != "secret" {
		t.Fatal("client_secret not sent correctly")
	}

	// An upstream error reply fails closed (no identity).
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":"100019","error_description":"redirect uri is illegal"}`))
	}))
	defer errSrv.Close()
	q.TokenBase = errSrv.URL
	if _, e := q.Exchange(context.Background(), "c"); e != ErrUpstream {
		t.Fatal("error reply must be ErrUpstream", e)
	}
	// A non-2xx fails closed.
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer badSrv.Close()
	q.TokenBase = badSrv.URL
	if _, e := q.Exchange(context.Background(), "c"); e != ErrUpstream {
		t.Fatal("500 must be ErrUpstream", e)
	}
	// Missing openid fails closed.
	noOpen := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"access_token":"AT"}`))
	}))
	defer noOpen.Close()
	q.TokenBase = noOpen.URL
	if _, e := q.Exchange(context.Background(), "c"); e != ErrUpstream {
		t.Fatal("missing openid must be ErrUpstream", e)
	}
	// Unconfigured / empty code fail closed.
	if _, e := (QQ{}).Exchange(context.Background(), "c"); e != ErrConfig {
		t.Fatal("unconfigured exchange should be ErrConfig", e)
	}
	if _, e := q.Exchange(context.Background(), ""); e != ErrUpstream {
		t.Fatal("empty code should be ErrUpstream", e)
	}
}
