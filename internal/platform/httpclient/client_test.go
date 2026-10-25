package httpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
)

func TestRejectPrivateAndMappedNetworks(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.1.1", "172.16.1.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "224.0.0.1", "198.18.0.1", "192.0.2.1", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "2001:db8::1", "64:ff9b::7f00:1"} {
		if publicIP(net.ParseIP(ip)) {
			t.Errorf("accepted private/reserved %s", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicIP(net.ParseIP(ip)) {
			t.Errorf("public rejected %s", ip)
		}
	}
}
func TestURLRules(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "http://127.0.0.1/x", "https://[::ffff:127.0.0.1]/", "http://user:pass@example.com/", "http://example.com/#frag", "http://localhost", "http://service.local", "http://foo.internal", "http://example.com:0"} {
		if err := ValidateURL(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if err := ValidateURL("https://example.com/path?q=1"); err != nil {
		t.Fatal(err)
	}
}

type resolverFunc func(context.Context, string, string) ([]net.IP, error)

func (f resolverFunc) LookupIP(c context.Context, n, h string) ([]net.IP, error) { return f(c, n, h) }
func TestDNSRebindingAndMixedAnswerDenied(t *testing.T) {
	calls := 0
	guard := safeDialer{resolver: resolverFunc(func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("127.0.0.1")}, nil
	}), dial: func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("unexpected dial")
	}}
	if _, err := guard.DialContext(context.Background(), "tcp", "example.com:80"); err == nil {
		t.Fatal("mixed DNS accepted")
	}
	if calls != 0 {
		t.Fatal("dialed before validating all addresses")
	}
	var target string
	lookups := 0
	guard.resolver = resolverFunc(func(context.Context, string, string) ([]net.IP, error) {
		lookups++
		if lookups > 1 {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		}
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	guard.dial = func(_ context.Context, _ string, address string) (net.Conn, error) {
		target = address
		return nil, errors.New("test dial stopped")
	}
	guard.DialContext(context.Background(), "tcp", "example.com:443")
	if lookups != 1 || target != "8.8.8.8:443" {
		t.Fatalf("DNS not pinned: lookups=%d target=%s", lookups, target)
	}
}
func TestClientTimeoutAndRedirect(t *testing.T) {
	client := New()
	if client.Timeout <= 0 {
		t.Fatal("no total request timeout")
	}
	if err := client.CheckRedirect(&http.Request{}, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect policy: %v", err)
	}
	transport := client.Transport.(*guardedTransport)
	if transport.base.Proxy != nil {
		t.Fatal("environment proxy bypasses destination policy")
	}
}
