package httpclient

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fixedResolver resolves every host to one public IP, so the pinned SOCKS
// target is deterministic without real DNS.
func fixedResolver(ip string) resolver {
	return resolverFunc(func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP(ip)}, nil
	})
}

// serveOnce accepts one connection, lets handle negotiate the proxy protocol,
// then replies with a fixed HTTP/1.1 200 and closes.
func serveOnce(t *testing.T, handle func(net.Conn)) (string, func(context.Context, string, string) (net.Conn, error)) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		handle(conn)
	}()
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, ln.Addr().String())
	}
	return ln.Addr().String(), dial
}

func writeHTTPOK(conn net.Conn) {
	io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 5\r\nConnection: close\r\n\r\nrelay")
}

func readRequestLine(t *testing.T, conn net.Conn) string {
	t.Helper()
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read tunnelled request: %v", err)
	}
	return strings.TrimSpace(line)
}

func TestProxySOCKS5LocalResolveNegotiatesPinnedTargetWithAuth(t *testing.T) {
	var sawAuth bool
	var targetIP net.IP
	var targetPort int
	_, dial := serveOnce(t, func(conn net.Conn) {
		hello := make([]byte, 2)
		io.ReadFull(conn, hello)
		methods := make([]byte, hello[1])
		io.ReadFull(conn, methods)
		conn.Write([]byte{0x05, 0x02}) // demand username/password
		head := make([]byte, 2)
		io.ReadFull(conn, head)
		user := make([]byte, head[1])
		io.ReadFull(conn, user)
		plen := make([]byte, 1)
		io.ReadFull(conn, plen)
		pass := make([]byte, plen[0])
		io.ReadFull(conn, pass)
		sawAuth = string(user) == "agent" && string(pass) == "s3cret"
		conn.Write([]byte{0x01, 0x00})
		req := make([]byte, 4)
		io.ReadFull(conn, req)
		ip := make([]byte, 4)
		io.ReadFull(conn, ip)
		targetIP = net.IP(ip)
		port := make([]byte, 2)
		io.ReadFull(conn, port)
		targetPort = int(binary.BigEndian.Uint16(port))
		conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		readRequestLine(t, conn)
		writeHTTPOK(conn)
	})
	client, err := newProxied(ProxyConfig{Type: "sock5", Server: "127.0.0.1", Port: "1", User: "agent", Password: "s3cret"}, fixedResolver("93.184.216.34"), dial)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get("http://merchant.example/callback")
	if err != nil {
		t.Fatalf("request through SOCKS5 proxy failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "relay" || !sawAuth || !targetIP.Equal(net.ParseIP("93.184.216.34")) || targetPort != 80 {
		t.Fatalf("SOCKS5 negotiation wrong: body=%q auth=%v ip=%v port=%d", body, sawAuth, targetIP, targetPort)
	}
}

func TestProxySOCKS4SendsResolvedIPv4(t *testing.T) {
	var targetIP net.IP
	_, dial := serveOnce(t, func(conn net.Conn) {
		head := make([]byte, 8)
		io.ReadFull(conn, head)
		targetIP = net.IPv4(head[4], head[5], head[6], head[7])
		// consume the NUL-terminated userid
		one := make([]byte, 1)
		for {
			if _, err := io.ReadFull(conn, one); err != nil || one[0] == 0 {
				break
			}
		}
		conn.Write([]byte{0x00, 0x5a, 0, 0, 0, 0, 0, 0})
		readRequestLine(t, conn)
		writeHTTPOK(conn)
	})
	client, err := newProxied(ProxyConfig{Type: "sock4", Server: "127.0.0.1", Port: "1"}, fixedResolver("93.184.216.34"), dial)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get("http://merchant.example/callback")
	if err != nil {
		t.Fatalf("request through SOCKS4 proxy failed: %v", err)
	}
	resp.Body.Close()
	if !targetIP.Equal(net.ParseIP("93.184.216.34").To4()) {
		t.Fatalf("SOCKS4 sent wrong target %v", targetIP)
	}
}

func TestProxyHTTPConnectTunnelsHTTPSAndSendsProxyAuth(t *testing.T) {
	var connectTarget, proxyAuth string
	_, dial := serveOnce(t, func(conn net.Conn) {
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "CONNECT ") {
				connectTarget = strings.Fields(trimmed)[1]
			}
			if strings.HasPrefix(strings.ToLower(trimmed), "proxy-authorization:") {
				proxyAuth = strings.TrimSpace(trimmed[len("proxy-authorization:"):])
			}
			if trimmed == "" {
				break
			}
		}
		// Refuse the tunnel after proving the request: end-to-end TLS to a real
		// upstream is out of scope for this handshake double.
		io.WriteString(conn, "HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\n\r\n")
	})
	client, err := newProxied(ProxyConfig{Type: "http", Server: "127.0.0.1", Port: "1", User: "agent", Password: "s3cret"}, net.DefaultResolver, dial)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://merchant.example/callback", nil)
	if _, err := client.Do(req); err == nil {
		t.Fatal("expected the refused tunnel to surface as an error")
	}
	if connectTarget != "merchant.example:443" || !strings.HasPrefix(proxyAuth, "Basic ") {
		t.Fatalf("CONNECT target=%q proxy-auth=%q", connectTarget, proxyAuth)
	}
}

func TestProxyStillRefusesPrivateFinalTargetAndBadConfig(t *testing.T) {
	_, dial := serveOnce(t, func(conn net.Conn) { conn.Close() })
	// A private/loopback literal target is refused by ValidateURL before any
	// proxy dial, in every mode.
	for _, kind := range []string{"http", "https", "sock4", "sock5", "sock5h"} {
		client, err := newProxied(ProxyConfig{Type: kind, Server: "127.0.0.1", Port: "1"}, fixedResolver("93.184.216.34"), dial)
		if err != nil {
			t.Fatal(kind, err)
		}
		if _, err := client.Get("http://127.0.0.1/x"); err == nil {
			t.Fatalf("%s accepted a loopback target", kind)
		}
	}
	// A hostname that resolves to a private address is refused for the
	// local-resolve SOCKS types before the proxy is contacted.
	privateDial := func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dialed the proxy for a private resolution")
		return nil, nil
	}
	client, _ := newProxied(ProxyConfig{Type: "sock5", Server: "127.0.0.1", Port: "1"}, fixedResolver("10.0.0.5"), privateDial)
	if _, err := client.Get("https://merchant.example/callback"); err == nil {
		t.Fatal("SOCKS5 reached a target resolving to a private IP")
	}
	for _, bad := range []ProxyConfig{
		{Type: "socks5", Server: "p", Port: "1"},
		{Type: "sock5", Server: "", Port: "1"},
		{Type: "sock5", Server: "p", Port: "0"},
		{Type: "sock5", Server: "p", Port: "70000"},
		{Type: "sock5", Server: "has space", Port: "1"},
	} {
		if _, err := NewProxied(bad); err != ErrProxyConfig {
			t.Fatalf("accepted invalid proxy config %+v", bad)
		}
	}
}
