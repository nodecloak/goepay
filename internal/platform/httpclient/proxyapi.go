// 中转代理 proxy mode 2 (original do_notify proxy API branch). Instead of
// dialing the merchant through a proxy, the platform POSTs the merchant URL to
// an operator-run relay site, which fetches it and returns the merchant's
// acknowledgement. The relay URL is operator-chosen infrastructure (set only by
// a super admin), so it is reached with a plain bounded client, like the mode-1
// proxy server, rather than the SSRF-pinned webhook client.
package httpclient

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ProxyAPIClient signs and sends a merchant URL through a mode-2 relay.
type ProxyAPIClient struct {
	client *http.Client
	now    func() time.Time
}

// NewProxyAPIClient builds the relay client. apiURL is validated as an http(s)
// URL but, like the proxy server, is NOT SSRF-filtered: it is trusted operator
// infrastructure that may legitimately be internal.
func NewProxyAPIClient() *ProxyAPIClient {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := tunedTransport(nil, dialer.DialContext)
	return &ProxyAPIClient{client: &http.Client{Timeout: 10 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now}
}

func validAPIURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.Fragment == "" && len(raw) <= 2048
}

// Deliver POSTs url=base64(target), timestamp, sign=md5(url_b64+timestamp+
// apikey) to the relay and returns its response body. The md5 signature is NOT
// a security choice of ours: it is the fixed wire contract of the original
// self-hosted proxy-API source, kept only so existing relays still verify.
func (c *ProxyAPIClient) Deliver(ctx context.Context, apiURL, apikey, target string) (int, string, error) {
	if !validAPIURL(apiURL) || apikey == "" || len(apikey) > 256 || ValidateURL(target) != nil {
		return 0, "", ErrProxyConfig
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(target))
	timestamp := strconv.FormatInt(c.now().Unix(), 10)
	sum := md5.Sum([]byte(encoded + timestamp + apikey))
	form := url.Values{"url": {encoded}, "timestamp": {timestamp}, "sign": {hex.EncodeToString(sum[:])}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, "", ErrProxyConfig
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "GOEPAY-Notify/1.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(raw), nil
}

// ProxyAPIAcknowledged matches the original do_notify success test on the relay
// body (contains success / SUCCESS / Success). It applies ONLY to the mode-2
// relay response — the direct webhook ack stays an exact "success" match.
func ProxyAPIAcknowledged(body string) bool {
	return strings.Contains(body, "success") || strings.Contains(body, "SUCCESS") || strings.Contains(body, "Success")
}

var _ = tls.VersionTLS12
