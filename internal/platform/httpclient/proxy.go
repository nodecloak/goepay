// 中转代理 (original admin/set.php mod=proxy, proxy mode 1). The merchant
// async-notify request is sent THROUGH an operator-chosen proxy, to reach a
// site only the proxy's network can see and to hide the platform's own egress
// IP. The proxy address itself is operator-chosen, so it is never SSRF-checked;
// the FINAL merchant URL still must pass ValidateURL, and for the local-resolve
// SOCKS types every resolved target IP must still be public.
package httpclient

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrProxyConfig = errors.New("invalid outbound proxy configuration")

// ProxyConfig is the resolved 中转代理 server setting. Type is one of the
// original http / https / sock4 / sock5 / sock5h.
type ProxyConfig struct {
	Type, Server, Port, User, Password string
}

func (p ProxyConfig) valid() bool {
	switch p.Type {
	case "http", "https", "sock4", "sock5", "sock5h":
	default:
		return false
	}
	if p.Server == "" || len(p.Server) > 255 || strings.ContainsAny(p.Server, "\x00\r\n\t /@") {
		return false
	}
	n, e := strconv.Atoi(p.Port)
	if e != nil || n < 1 || n > 65535 {
		return false
	}
	for _, v := range []string{p.User, p.Password} {
		if len(v) > 255 || strings.ContainsAny(v, "\x00\r\n") {
			return false
		}
	}
	return true
}

func (p ProxyConfig) address() string { return net.JoinHostPort(p.Server, p.Port) }

// NewProxied builds a bounded client that routes through the proxy. It keeps
// every protection New has on the final merchant URL (ValidateURL + redirect
// refusal + TLS floor + timeouts).
func NewProxied(p ProxyConfig) (*http.Client, error) {
	return newProxied(p, net.DefaultResolver, (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext)
}

func newProxied(p ProxyConfig, res resolver, dialProxy func(context.Context, string, string) (net.Conn, error)) (*http.Client, error) {
	if !p.valid() {
		return nil, ErrProxyConfig
	}
	var transport *http.Transport
	switch p.Type {
	case "http", "https", "sock5h":
		scheme := p.Type
		if p.Type == "sock5h" {
			scheme = "socks5" // the std library's socks5 passes the hostname (remote resolution)
		}
		proxyURL := &url.URL{Scheme: scheme, Host: p.address()}
		if p.User != "" || p.Password != "" {
			proxyURL.User = url.UserPassword(p.User, p.Password)
		}
		// The std transport dials the PROXY through this dialer (plain: the
		// operator-chosen proxy is not SSRF-filtered) and then issues CONNECT
		// (https target) or forwards (http target) / negotiates SOCKS5.
		transport = tunedTransport(http.ProxyURL(proxyURL), dialProxy)
	case "sock4", "sock5":
		d := socksDialer{v5: p.Type == "sock5", proxy: p.address(), user: p.User, resolver: res, dialProxy: dialProxy}
		if p.Type == "sock5" {
			d.pass = p.Password
		}
		transport = tunedTransport(nil, d.DialContext)
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: &guardedTransport{base: transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

// socksDialer reaches the final target through a SOCKS4/SOCKS5 proxy, resolving
// the target locally and pinning it to a public IP (sock4/sock5 = local
// resolution). sock5h is handled by the std library instead. DialContext
// receives the FINAL target host:port because Transport.Proxy is nil. It reads
// handshake bytes straight off the connection (no buffering) so no byte of the
// tunnelled stream is ever read ahead and lost.
type socksDialer struct {
	v5         bool
	proxy      string
	user, pass string
	resolver   resolver
	dialProxy  func(context.Context, string, string) (net.Conn, error)
}

func (d socksDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrDestination
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, ErrDestination
	}
	family := "ip"
	if !d.v5 {
		family = "ip4" // SOCKS4 is IPv4-only
	}
	ips, err := d.resolver.LookupIP(ctx, family, host)
	if err != nil {
		return nil, fmt.Errorf("resolve webhook host: %w", err)
	}
	if len(ips) == 0 {
		return nil, ErrDestination
	}
	// Pin to the public resolution: a target that resolves to a private or
	// reserved address is refused before the proxy is contacted.
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, ErrDestination
		}
	}
	var target net.IP
	for _, ip := range ips {
		if d.v5 || ip.To4() != nil {
			target = ip
			break
		}
	}
	if target == nil {
		return nil, ErrDestination
	}
	conn, err := d.dialProxy(ctx, "tcp", d.proxy)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	if err = d.negotiate(conn, target, port); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return conn, nil
}

func (d socksDialer) negotiate(conn net.Conn, target net.IP, port int) error {
	if d.v5 {
		return d.negotiate5(conn, target, port)
	}
	return d.negotiate4(conn, target, port)
}

func (d socksDialer) negotiate5(conn net.Conn, target net.IP, port int) error {
	methods := []byte{0x00}
	if d.user != "" || d.pass != "" {
		methods = []byte{0x02, 0x00}
	}
	hello := append([]byte{0x05, byte(len(methods))}, methods...)
	if _, err := conn.Write(hello); err != nil {
		return err
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[0] != 0x05 {
		return ErrProxyConfig
	}
	switch reply[1] {
	case 0x00:
	case 0x02:
		auth := []byte{0x01, byte(len(d.user))}
		auth = append(auth, d.user...)
		auth = append(auth, byte(len(d.pass)))
		auth = append(auth, d.pass...)
		if _, err := conn.Write(auth); err != nil {
			return err
		}
		status := make([]byte, 2)
		if _, err := io.ReadFull(conn, status); err != nil {
			return err
		}
		if status[0] != 0x01 || status[1] != 0x00 {
			return errors.New("proxy rejected SOCKS5 credentials")
		}
	default:
		return errors.New("proxy offered no acceptable SOCKS5 auth method")
	}
	request := []byte{0x05, 0x01, 0x00}
	if v4 := target.To4(); v4 != nil {
		request = append(request, 0x01)
		request = append(request, v4...)
	} else {
		request = append(request, 0x04)
		request = append(request, target.To16()...)
	}
	request = binary.BigEndian.AppendUint16(request, uint16(port))
	if _, err := conn.Write(request); err != nil {
		return err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[0] != 0x05 {
		return ErrProxyConfig
	}
	if head[1] != 0x00 {
		return fmt.Errorf("SOCKS5 proxy refused the target (reply 0x%02x)", head[1])
	}
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4
	case 0x04:
		skip = 16
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return err
		}
		skip = int(length[0])
	default:
		return ErrProxyConfig
	}
	if _, err := io.CopyN(io.Discard, conn, int64(skip+2)); err != nil {
		return err
	}
	return nil
}

func (d socksDialer) negotiate4(conn net.Conn, target net.IP, port int) error {
	v4 := target.To4()
	if v4 == nil {
		return ErrDestination
	}
	request := []byte{0x04, 0x01}
	request = binary.BigEndian.AppendUint16(request, uint16(port))
	request = append(request, v4...)
	request = append(request, d.user...)
	request = append(request, 0x00)
	if _, err := conn.Write(request); err != nil {
		return err
	}
	reply := make([]byte, 8)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[0] != 0x00 {
		return ErrProxyConfig
	}
	if reply[1] != 0x5a {
		return fmt.Errorf("SOCKS4 proxy refused the target (reply 0x%02x)", reply[1])
	}
	return nil
}
