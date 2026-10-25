package payment

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A real TLS server with a synthetic certificate for the fixed official host.
// Only its TCP dial is mapped locally; hostname verification stays enabled.
func miniLinkTLS(t *testing.T, handler http.Handler) (*httptest.Server, *http.Client) {
	t.Helper()
	key := testKey(t)
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "api.weixin.qq.com synthetic only"}, DNSNames: []string{"api.weixin.qq.com"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	localURL, _ := url.Parse(server.URL)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, Proxy: func(*http.Request) (*url.URL, error) {
		t.Error("proxy must not be used")
		return nil, errors.New("proxy disabled")
	}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "api.weixin.qq.com:443" {
			t.Error("escaped official hostname")
			return nil, ErrAuthentication
		}
		return (&net.Dialer{}).DialContext(ctx, network, localURL.Host)
	}}
	jar, _ := cookiejar.New(nil)
	official, _ := url.Parse("https://api.weixin.qq.com")
	jar.SetCookies(official, []*http.Cookie{{Name: "must-not-send", Value: "synthetic-cookie"}})
	return server, &http.Client{Transport: transport, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { t.Error("redirect policy was retained"); return nil }}
}

func miniLinkRequestFixture(now time.Time) WechatMiniLinkRequest {
	return WechatMiniLinkRequest{RequestID: strings.Repeat("a", 64), State: strings.Repeat("b", 64), CreatedAt: now, Deadline: now.Add(5 * time.Minute)}
}

func TestWechatMiniIdentityLinkFixedTLSProtocolAndRequestedExpiry(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	in := miniLinkRequestFixture(now)
	var calls atomic.Int32
	vendorURL := "https://wxaurl.cn/synthetic-ticket"
	_, client := miniLinkTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.Host != "api.weixin.qq.com" || r.TLS == nil || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("unsafe mini link request")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid request JSON")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "must-not-send=upstream; Secure; Path=/")
		switch r.URL.Path {
		case "/cgi-bin/stable_token":
			if r.URL.RawQuery != "" || len(body) != 4 || body["grant_type"] != "client_credential" || body["appid"] != "wx0123456789abcdef" || body["secret"] != "synthetic-mini-secret" || body["force_refresh"] != false {
				t.Error("wrong original app credentials or refresh mode")
			}
			io.WriteString(w, `{"access_token":"synthetic-access+token/&","expires_in":7200}`)
		case "/wxa/generate_urllink":
			if len(r.URL.Query()) != 1 || r.URL.Query().Get("access_token") != "synthetic-access+token/&" || len(body) != 5 || body["path"] != "pages/identity/index" || body["query"] != "request_id="+in.RequestID+"&state="+in.State || body["expire_type"] != float64(0) || body["expire_time"] != float64(in.Deadline.Unix()) || body["env_version"] != "release" {
				t.Error("wrong identity purpose/path/query/original expiry")
			}
			json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok", "url_link": vendorURL})
		default:
			t.Error("non-identity API called")
			w.WriteHeader(404)
		}
	}))
	p, err := NewWechatMiniLink(WechatMiniLinkConfig{AppID: "wx0123456789abcdef", AppSecret: "synthetic-mini-secret", IdentityPath: "pages/identity/index", HTTPClient: client, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("constructor made HTTP request")
	}
	for _, link := range []string{"https://wxaurl.cn/synthetic-ticket", "https://wxmpurl.cn/synthetic-ticket?cq=opaque%3Dvalue%26part%3D1"} {
		vendorURL = link
		out, err := p.GenerateIdentityLink(context.Background(), in)
		if err != nil || out.Provider != "wxpay" || out.AppID != "wx0123456789abcdef" || out.RequestID != in.RequestID || out.URLLink != link || !out.RequestedExpiresAt.Equal(in.Deadline) {
			t.Fatal("wrong identity link result", err)
		}
		raw, err := json.Marshal(out)
		if err != nil || strings.Contains(string(raw), "synthetic-access") || strings.Contains(string(raw), "synthetic-mini-secret") || strings.Contains(string(raw), "expires_in") {
			t.Fatal("credential lifetime or secret leaked into result")
		}
	}
	if calls.Load() != 4 {
		t.Fatal("wrong number of attempts")
	}
	var capability any = p
	if _, ok := capability.(Provider); ok {
		t.Fatal("link client exposes payment methods")
	}
	if _, ok := capability.(MiniOAuthProvider); ok {
		t.Fatal("link client exposes session code exchange")
	}
	if _, ok := capability.(WebOAuthProvider); ok {
		t.Fatal("link client exposes web OAuth")
	}
	raw, _ := json.Marshal(WechatMiniLinkConfig{AppID: "wx0123456789abcdef", AppSecret: "synthetic-mini-secret"})
	if strings.Contains(string(raw), "synthetic-mini-secret") {
		t.Fatal("config secret serialized")
	}
}

func TestWechatMiniIdentityLinkRejectsUntrustedInputBeforeIO(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var calls atomic.Int32
	_, client := miniLinkTLS(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	config := WechatMiniLinkConfig{AppID: "wx0123456789abcdef", AppSecret: "synthetic-mini-secret", IdentityPath: "pages/identity/index", HTTPClient: client, Now: func() time.Time { return now }}
	p, err := NewWechatMiniLink(config)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*WechatMiniLinkRequest){
		"request": func(r *WechatMiniLinkRequest) { r.RequestID = "x" }, "uppercase_state": func(r *WechatMiniLinkRequest) { r.State = strings.Repeat("A", 64) }, "query_injection": func(r *WechatMiniLinkRequest) { r.State = strings.Repeat("b", 64) + "&amount=1" },
		"future_born": func(r *WechatMiniLinkRequest) { r.CreatedAt = now.Add(time.Second) }, "zero_born": func(r *WechatMiniLinkRequest) { r.CreatedAt = time.Time{} }, "too_long": func(r *WechatMiniLinkRequest) { r.Deadline = r.CreatedAt.Add(5*time.Minute + time.Second) },
		"expired": func(r *WechatMiniLinkRequest) { r.CreatedAt = now.Add(-5 * time.Minute); r.Deadline = now }, "minimum_vendor_window": func(r *WechatMiniLinkRequest) { r.Deadline = now.Add(time.Minute) }, "insufficient_network_margin": func(r *WechatMiniLinkRequest) { r.Deadline = now.Add(70 * time.Second) },
		"fraction_floor": func(r *WechatMiniLinkRequest) { r.Deadline = now.Add(70*time.Second + 999*time.Millisecond) },
	} {
		t.Run(name, func(t *testing.T) {
			in := miniLinkRequestFixture(now)
			change(&in)
			if out, err := p.GenerateIdentityLink(context.Background(), in); err == nil || out.URLLink != "" {
				t.Fatal("invalid original request accepted")
			}
		})
	}
	for _, path := range []string{"", "/pages/identity/index", "pages//identity/index", "pages/../pay", "pages/identity?amount=1", "https://evil.example.invalid/path", "pages/identity#x", "pages/%2e%2e/pay"} {
		changed := config
		changed.IdentityPath = path
		if _, err := NewWechatMiniLink(changed); err == nil {
			t.Fatal("untrusted identity path accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input made HTTP request")
	}
}

func TestWechatMiniIdentityLinkRechecksOriginalDeadlineAfterToken(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	var elapsed atomic.Int64
	var tokenCalls, linkCalls atomic.Int32
	_, client := miniLinkTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/stable_token" {
			tokenCalls.Add(1)
			elapsed.Store(231)
			io.WriteString(w, `{"access_token":"synthetic-access","expires_in":7200}`)
			return
		}
		linkCalls.Add(1)
	}))
	p, err := NewWechatMiniLink(WechatMiniLinkConfig{AppID: "wx0123456789abcdef", AppSecret: "synthetic-mini-secret", IdentityPath: "pages/identity/index", HTTPClient: client, Now: func() time.Time { return base.Add(time.Duration(elapsed.Load()) * time.Second) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.GenerateIdentityLink(context.Background(), miniLinkRequestFixture(base)); !errors.Is(err, ErrWechatMiniLinkLifetime) {
		t.Fatal("short original deadline did not fail closed", err)
	}
	if tokenCalls.Load() != 1 || linkCalls.Load() != 0 {
		t.Fatal("expired request generated a link")
	}
}

func TestWechatMiniIdentityLinkRejectsMalformedTokenWithoutGeneratingLink(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for name, body := range map[string]string{
		"missing_expiry": `{"access_token":"synthetic-access"}`, "zero_expiry": `{"access_token":"synthetic-access","expires_in":0}`, "long_expiry": `{"access_token":"synthetic-access","expires_in":7201}`, "string_expiry": `{"access_token":"synthetic-access","expires_in":"7200"}`, "duplicate": `{"access_token":"one","access_token":"two","expires_in":7200}`, "nested_duplicate": `{"access_token":"synthetic-access","expires_in":7200,"extra":{"x":1,"x":2}}`, "empty_token": `{"access_token":"","expires_in":7200}`, "excluded_token": `{"access_token":"@bad","expires_in":7200}`, "wrong_errcode": `{"errcode":"0","access_token":"synthetic-access","expires_in":7200}`, "rejected": `{"errcode":40013,"errmsg":"synthetic-mini-secret synthetic-access"}`, "oversize": strings.Repeat(" ", 65537), "trailing": `{} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			_, client := miniLinkTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/cgi-bin/stable_token" {
					t.Error("token failure generated link")
				}
				io.WriteString(w, body)
			}))
			p, err := NewWechatMiniLink(WechatMiniLinkConfig{AppID: "wx0123456789abcdef", AppSecret: "synthetic-mini-secret", IdentityPath: "pages/identity/index", HTTPClient: client, Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			out, err := p.GenerateIdentityLink(context.Background(), miniLinkRequestFixture(now))
			var unknown *WechatMiniLinkUnknownError
			if err == nil || errors.As(err, &unknown) || out.URLLink != "" || strings.Contains(err.Error(), "synthetic") || calls.Load() != 1 {
				t.Fatal("untrusted token response accepted, leaked, retried or marked link unknown")
			}
		})
	}
}

func TestWechatMiniIdentityLinkMarksAmbiguousLinkResponseUnknownWithoutRetry(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for name, body := range map[string]string{
		"missing_errcode": `{"url_link":"https://wxaurl.cn/ticket"}`, "string_errcode": `{"errcode":"0","url_link":"https://wxaurl.cn/ticket"}`, "duplicate": `{"errcode":0,"url_link":"https://wxaurl.cn/first","url_link":"https://wxaurl.cn/second"}`, "oversize": strings.Repeat(" ", 65537), "plaintext": "https://wxaurl.cn/ticket", "missing_link": `{"errcode":0}`, "null_link": `{"errcode":0,"url_link":null}`,
		"invalid_utf8": "{\"errcode\":0,\"url_link\":\"https://wxaurl.cn/\xff\"}",
	} {
		t.Run(name, func(t *testing.T) {
			var tokenCalls, linkCalls atomic.Int32
			_, client := miniLinkTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/cgi-bin/stable_token" {
					tokenCalls.Add(1)
					io.WriteString(w, `{"access_token":"synthetic-access","expires_in":7200}`)
					return
				}
				linkCalls.Add(1)
				io.WriteString(w, body)
			}))
			p, err := NewWechatMiniLink(WechatMiniLinkConfig{AppID: "wx0123456789abcdef", AppSecret: "synthetic-mini-secret", IdentityPath: "pages/identity/index", HTTPClient: client, Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			out, err := p.GenerateIdentityLink(context.Background(), miniLinkRequestFixture(now))
			var unknown *WechatMiniLinkUnknownError
			if !errors.As(err, &unknown) || out.URLLink != "" || strings.Contains(err.Error(), "synthetic") || tokenCalls.Load() != 1 || linkCalls.Load() != 1 {
				t.Fatal("ambiguous link response was accepted, leaked or retried")
			}
		})
	}
}

type miniLinkForbiddenTransport struct{ calls *atomic.Int32 }

func (t miniLinkForbiddenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return nil, ErrAuthentication
}

func TestWechatMiniIdentityLinkTransportRejectsTLSBypassesAndNeverReplaysBody(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var calls, alternateCalls atomic.Int32
	_, client := miniLinkTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/cgi-bin/stable_token" {
			io.WriteString(w, `{"access_token":"synthetic-access","expires_in":7200}`)
			return
		}
		io.WriteString(w, `{"errcode":0,"url_link":"https://wxaurl.cn/ticket"}`)
	}))
	config := WechatMiniLinkConfig{AppID: "wx0123456789abcdef", AppSecret: "synthetic-mini-secret", IdentityPath: "pages/identity/index", HTTPClient: client, Now: func() time.Time { return now }}
	// An alternate HTTPS protocol handler must not survive transport ownership.
	client.Transport.(*http.Transport).RegisterProtocol("https", miniLinkForbiddenTransport{&alternateCalls})
	p, err := NewWechatMiniLink(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.GenerateIdentityLink(context.Background(), miniLinkRequestFixture(now)); err != nil || calls.Load() != 2 || alternateCalls.Load() != 0 {
		t.Fatal("alternate protocol bypassed fixed TLS transport", err)
	}
	req, err := wechatMiniLinkPostRequest(context.Background(), wechatMiniLinkStableEndpoint, map[string]string{"secret": "synthetic-mini-secret"})
	if err != nil || req.GetBody != nil || req.ContentLength <= 0 || !req.Close {
		t.Fatal("POST can be replayed by transport")
	}
	req.Body.Close()
	for _, transport := range []http.RoundTripper{
		miniLinkForbiddenTransport{&alternateCalls},
		&http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		&http.Transport{TLSClientConfig: &tls.Config{ServerName: "evil.example.invalid"}},
		&http.Transport{DialTLSContext: func(context.Context, string, string) (net.Conn, error) { return nil, ErrAuthentication }},
	} {
		changed := config
		changed.HTTPClient = &http.Client{Transport: transport}
		if _, err := NewWechatMiniLink(changed); err == nil {
			t.Fatal("unsafe transport accepted")
		}
	}
	// A supplied local dial is not a way to skip certificate verification.
	client.Transport.(*http.Transport).TLSClientConfig.RootCAs = x509.NewCertPool()
	// Remove the alternate handler by constructing a fresh allowed standard base.
	client.Transport = &http.Transport{DialContext: client.Transport.(*http.Transport).DialContext, TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool()}}
	p, err = NewWechatMiniLink(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.GenerateIdentityLink(context.Background(), miniLinkRequestFixture(now)); err == nil || calls.Load() != 2 {
		t.Fatal("untrusted TLS server accepted or called API")
	}
}

func TestWechatMiniIdentityLinkDoesNotRetryRedirectDropServerErrorOrVendorRejection(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, stage := range []string{"token", "link"} {
		for _, mode := range []string{"redirect", "drop", "server_error", "vendor_rejected"} {
			t.Run(stage+"_"+mode, func(t *testing.T) {
				var tokenCalls, linkCalls atomic.Int32
				_, client := miniLinkTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					io.Copy(io.Discard, r.Body)
					token := r.URL.Path == "/cgi-bin/stable_token"
					if token {
						tokenCalls.Add(1)
					} else {
						linkCalls.Add(1)
					}
					if stage == "link" && token {
						io.WriteString(w, `{"access_token":"synthetic-access","expires_in":7200}`)
						return
					}
					switch mode {
					case "redirect":
						w.Header().Set("Location", "https://api.weixin.qq.com/should-never-follow")
						w.WriteHeader(307)
					case "drop":
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						conn.Close()
					case "server_error":
						w.WriteHeader(503)
						io.WriteString(w, "synthetic-mini-secret synthetic-access")
					case "vendor_rejected":
						io.WriteString(w, `{"errcode":40001,"errmsg":"synthetic-mini-secret synthetic-access"}`)
					}
				}))
				p, err := NewWechatMiniLink(WechatMiniLinkConfig{AppID: "wx0123456789abcdef", AppSecret: "synthetic-mini-secret", IdentityPath: "pages/identity/index", HTTPClient: client, Now: func() time.Time { return now }})
				if err != nil {
					t.Fatal(err)
				}
				out, err := p.GenerateIdentityLink(context.Background(), miniLinkRequestFixture(now))
				var unknown *WechatMiniLinkUnknownError
				wantUnknown := stage == "link" && mode != "vendor_rejected"
				if err == nil || errors.As(err, &unknown) != wantUnknown || strings.Contains(err.Error(), "synthetic") || out.URLLink != "" || tokenCalls.Load() != 1 || linkCalls.Load() != int32(map[bool]int{true: 1, false: 0}[stage == "link"]) {
					t.Fatal("unsafe outcome, retry, redirect or secret error")
				}
				if mode == "vendor_rejected" && !errors.Is(err, ErrWechatMiniLinkRejected) {
					t.Fatal("known provider rejection became success or unknown")
				}
			})
		}
	}
}

func TestWechatMiniIdentityLinkBoundsEntireTwoPOSTOperation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var tokenCalls, linkCalls atomic.Int32
	_, client := miniLinkTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/cgi-bin/stable_token" {
			tokenCalls.Add(1)
		} else {
			linkCalls.Add(1)
		}
		time.Sleep(200 * time.Millisecond)
		if r.URL.Path == "/cgi-bin/stable_token" {
			io.WriteString(w, `{"access_token":"synthetic-access","expires_in":7200}`)
			return
		}
		io.WriteString(w, `{"errcode":0,"url_link":"https://wxaurl.cn/ticket"}`)
	}))
	client.Timeout = 350 * time.Millisecond
	p, err := NewWechatMiniLink(WechatMiniLinkConfig{AppID: "wx0123456789abcdef", AppSecret: "synthetic-mini-secret", IdentityPath: "pages/identity/index", HTTPClient: client, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.GenerateIdentityLink(context.Background(), miniLinkRequestFixture(now))
	var unknown *WechatMiniLinkUnknownError
	if !errors.As(err, &unknown) || out.URLLink != "" || tokenCalls.Load() != 1 || linkCalls.Load() != 1 {
		t.Fatal("timeout was reset per request, retried or returned link")
	}
}

func TestWechatMiniIdentityLinkRejectsUnsafeVendorLinksAsUnknown(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, link := range []string{
		"https://evil.example.invalid/ticket", "https://sub.wxaurl.cn/ticket", "http://wxaurl.cn/ticket", "https://wxaurl.cn:443/ticket", "https://@wxaurl.cn/ticket", "https://:@wxaurl.cn/ticket", "https://user:pass@wxaurl.cn/ticket", "https://wxaurl.cn/ticket#", "https://wxaurl.cn/ticket?", "https://wxaurl.cn/", "https://wxaurl.cn/ticket\n", "https://wxaurl.cn/%0a", "https://wxaurl.cn/%2e%2e/pay", "https://wxaurl.cn/ticket?access_token=synthetic-access", "https://wxaurl.cn/ticket?cq=one&cq=two", "https://wxaurl.cn/ticket?cq=%0a", "https://wxaurl.cn/ticket?cq=" + strings.Repeat("a", 257), "https://wxaurl.cn//",
	} {
		t.Run(link, func(t *testing.T) {
			var calls atomic.Int32
			_, client := miniLinkTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/cgi-bin/stable_token" {
					io.WriteString(w, `{"access_token":"synthetic-access","expires_in":7200}`)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok", "url_link": link})
			}))
			p, err := NewWechatMiniLink(WechatMiniLinkConfig{AppID: "wx0123456789abcdef", AppSecret: "synthetic-mini-secret", IdentityPath: "pages/identity/index", HTTPClient: client, Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			out, err := p.GenerateIdentityLink(context.Background(), miniLinkRequestFixture(now))
			var unknown *WechatMiniLinkUnknownError
			if !errors.As(err, &unknown) || out.URLLink != "" || calls.Load() != 2 {
				t.Fatal("unsafe vendor link accepted or retried")
			}
		})
	}
}
