package server

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestTrustedProxyBoundary(t *testing.T) {
	wrap, e := NewTrustedProxy("192.0.2.7/32,2001:db8:7::/64")
	if e != nil {
		t.Fatal(e)
	}
	var observed, transport string
	handler := wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = r.RemoteAddr
		transport = TransportPeer(r)
		w.WriteHeader(204)
	}))
	cases := []struct {
		name, peer, real, want string
		status                 int
	}{
		{"direct spoof", "192.0.2.8:456", "203.0.113.55", "192.0.2.8:456", 204},
		{"trusted ipv4", "192.0.2.7:456", "203.0.113.55", "203.0.113.55:456", 204},
		{"trusted ipv6", "[2001:db8:7::1]:456", "2001:0db8:0008::1", "[2001:db8:8::1]:456", 204},
		{"missing", "192.0.2.7:456", "", "", 400},
		{"chain", "192.0.2.7:456", "203.0.113.55, 203.0.113.56", "", 400},
		{"port", "192.0.2.7:456", "203.0.113.55:88", "", 400},
		{"zone", "192.0.2.7:456", "fe80::1%eth0", "", 400},
		{"unspecified", "192.0.2.7:456", "0.0.0.0", "", 400},
		{"mapped unspecified", "192.0.2.7:456", "::ffff:0.0.0.0", "", 400},
		{"multicast", "192.0.2.7:456", "ff02::1", "", 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Real-IP", tc.real)
			r.Header.Set("X-Forwarded-For", "198.51.100.99")
			w := httptest.NewRecorder()
			observed = ""
			handler.ServeHTTP(w, r)
			if w.Code != tc.status || observed != tc.want {
				t.Fatalf("%d %q", w.Code, observed)
			}
			if tc.status == 204 && transport != tc.peer {
				t.Fatal("original peer lost")
			}
		})
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.7:456"
	r.Header.Add("X-Real-IP", "203.0.113.55")
	r.Header.Add("X-Real-IP", "203.0.113.56")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("duplicate header accepted")
	}
	disabled, e := NewTrustedProxy("")
	if e != nil {
		t.Fatal(e)
	}
	r = httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.7:456"
	r.Header.Set("X-Real-IP", "203.0.113.55")
	disabled(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "192.0.2.7:456" {
			t.Fatal("default trusted headers")
		}
	})).ServeHTTP(httptest.NewRecorder(), r)
}

func TestTrustedProxyInvalidConfiguration(t *testing.T) {
	for _, raw := range []string{"0.0.0.0/0", "::/0", "*", "192.0.2.7", "192.0.2.7/33", "192.0.2.7/32,", "::ffff:192.0.2.7/128"} {
		if _, e := NewTrustedProxy(raw); e == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestUntrustedProxyHint(t *testing.T) {
	var hints []string
	previous := untrustedProxyHint
	untrustedProxyHint = func(peer netip.Addr) { hints = append(hints, peer.String()) }
	untrustedProxyLastHint.Store(0)
	defer func() {
		untrustedProxyHint = previous
		untrustedProxyLastHint.Store(0)
	}()
	wrap, e := NewTrustedProxy("")
	if e != nil {
		t.Fatal(e)
	}
	var observed string
	handler := wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = r.RemoteAddr
		w.WriteHeader(204)
	}))
	send := func(peer string, forwarded bool) {
		t.Helper()
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = peer
		if forwarded {
			r.Header.Set("X-Real-IP", "203.0.113.9")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 204 || observed != peer {
			t.Fatalf("untrusted proxy headers changed the request: %d %s", w.Code, observed)
		}
	}
	send("198.51.100.5:1000", true) // a public client may send anything: never a hint
	send("172.25.0.1:1000", false)  // a private peer without proxy headers
	if len(hints) != 0 {
		t.Fatal("unexpected hint", hints)
	}
	send("172.25.0.1:1000", true)
	send("127.0.0.1:1000", true) // rate limited
	if len(hints) != 1 || hints[0] != "172.25.0.1" {
		t.Fatal("misconfigured proxy hint not emitted exactly once", hints)
	}
	untrustedProxyLastHint.Store(time.Now().Add(-11 * time.Minute).UnixNano())
	send("[::1]:1000", true)
	if len(hints) != 2 || hints[1] != "::1" {
		t.Fatal("hint not repeated after the quiet period", hints)
	}
}

func TestCrossSiteLoginSubmissionsAreRefused(t *testing.T) {
	s := &Server{KYC233ConsoleOrigin: "https://console.example.com"}
	for _, tc := range []struct {
		name, site, origin string
		allowed            bool
	}{
		{"non-browser client", "", "", true},
		{"same-origin fetch", "same-origin", "https://pay.example.com", true},
		{"configured console origin", "same-origin", "https://console.example.com", true},
		{"cross-site form", "cross-site", "https://evil.example", false},
		{"sibling subdomain", "same-site", "https://evil.pay.example.com", false},
		{"foreign origin without fetch metadata", "", "https://evil.example", false},
		{"opaque origin", "", "null", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "https://pay.example.com/console/v1/admin/login", nil)
			if tc.site != "" {
				r.Header.Set("Sec-Fetch-Site", tc.site)
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := s.loginFromThisSite(r); got != tc.allowed {
				t.Fatalf("got %v, want %v", got, tc.allowed)
			}
		})
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "https://pay.example.com/console/v1/admin/login", strings.NewReader(`{"username":"admin","password":"x","code":"="}`))
	r.Header.Set("Content-Type", "text/plain")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Origin", "https://evil.example")
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 || w.Result().Header.Get("Set-Cookie") != "" {
		t.Fatalf("cross-site login reached authentication: %d", w.Code)
	}
}
