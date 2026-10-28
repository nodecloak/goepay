// Package httpclient provides a bounded outbound client for merchant webhooks.
// It validates DNS answers once and pins each dial to a validated public address.
package httpclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrDestination = errors.New("outbound destination is not a public HTTP endpoint")
var denied = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("::/96"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("fc00::/7"), netip.MustParsePrefix("fe80::/10"), netip.MustParsePrefix("ff00::/8"),
}

func publicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() {
		return false
	}
	for _, prefix := range denied {
		if prefix.Contains(addr) {
			return false
		}
	}
	// Only currently globally routable IPv6 unicast space. This also rejects
	// translation/tunnel ranges which can hide an embedded private IPv4 address.
	if addr.Is6() && !netip.MustParsePrefix("2000::/3").Contains(addr) {
		return false
	}
	return true
}
func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return ErrDestination
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return ErrDestination
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || strings.Contains(host, "%") || !strings.Contains(host, ".") && net.ParseIP(host) == nil {
		return ErrDestination
	}
	for _, suffix := range []string{".localhost", ".local", ".internal", ".home", ".lan"} {
		if strings.HasSuffix(host, suffix) {
			return ErrDestination
		}
	}
	if ip := net.ParseIP(host); ip != nil && !publicIP(ip) {
		return ErrDestination
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return ErrDestination
		}
	}
	return nil
}

type resolver interface {
	LookupIP(context.Context, string, string) ([]net.IP, error)
}
type safeDialer struct {
	resolver resolver
	dial     func(context.Context, string, string) (net.Conn, error)
}

func (d safeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrDestination
	}
	ips, err := d.resolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve webhook host: %w", err)
	}
	if len(ips) == 0 {
		return nil, ErrDestination
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, ErrDestination
		}
	}
	var last error
	for _, ip := range ips {
		conn, err := d.dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, last
}

type guardedTransport struct{ base *http.Transport }

func (t *guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || ValidateURL(req.URL.String()) != nil {
		return nil, ErrDestination
	}
	if req.Host != "" && req.Host != req.URL.Host {
		return nil, ErrDestination
	}
	return t.base.RoundTrip(req)
}
func (t *guardedTransport) CloseIdleConnections() { t.base.CloseIdleConnections() }

// tunedTransport returns the bounded transport shared by every builder: the
// same TLS floor, timeouts and connection caps, with the given proxy selector
// and dialer. Proxy is the function the std library consults for an HTTP/
// SOCKS5 proxy (nil = direct); dial is how the proxy (or, direct, the pinned
// target) is reached.
func tunedTransport(proxy func(*http.Request) (*url.URL, error), dial func(context.Context, string, string) (net.Conn, error)) *http.Transport {
	return &http.Transport{Proxy: proxy, DialContext: dial, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 32, MaxIdleConnsPerHost: 4, MaxConnsPerHost: 8, MaxResponseHeaderBytes: 32 << 10, ForceAttemptHTTP2: true}
}
func New() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	guard := safeDialer{resolver: net.DefaultResolver, dial: dialer.DialContext}
	transport := tunedTransport(nil, guard.DialContext)
	return &http.Client{Timeout: 10 * time.Second, Transport: &guardedTransport{base: transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// NewMutualTLS preserves the same URL, DNS and redirect protections as New.
func NewMutualTLS(cert tls.Certificate) *http.Client {
	client := New()
	transport := client.Transport.(*guardedTransport)
	transport.base.TLSClientConfig.Certificates = []tls.Certificate{cert}
	return client
}
