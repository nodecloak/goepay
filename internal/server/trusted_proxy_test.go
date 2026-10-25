package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
