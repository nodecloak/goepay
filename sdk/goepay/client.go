package goepay

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MaxResponseBytes is the maximum decoded JSON HTTP body accepted by the SDK.
const MaxResponseBytes = 1024 * 1024

// Config is immutable after New. The base must be an HTTPS origin with no path,
// credentials, query or fragment. Keys are PEM, never a URL or a filename.
type Config struct {
	BaseURL               string
	MerchantID            int64
	MerchantPrivateKeyPEM []byte
	PlatformPublicKeyPEM  []byte
	Timeout               time.Duration
	// AllowLoopbackHTTP permits only literal loopback IPs for local httptest.
	// It never permits plain HTTP to an external host or localhost DNS name.
	AllowLoopbackHTTP bool
}

type Client struct {
	origin     string
	merchantID string
	private    *rsa.PrivateKey
	public     *rsa.PublicKey
	http       *http.Client
	now        func() time.Time
}

// New parses keys and configures an isolated HTTP client. It makes no request.
func New(config Config) (*Client, error) {
	u, err := url.Parse(config.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.Opaque != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("goepay: BaseURL must be an HTTPS origin")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !config.AllowLoopbackHTTP || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("goepay: HTTPS required")
		}
	}
	if config.MerchantID <= 0 || config.Timeout < 0 {
		return nil, errors.New("goepay: invalid merchant or timeout")
	}
	priv, err := privateKey(config.MerchantPrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	pub, err := publicKey(config.PlatformPublicKeyPEM)
	if err != nil {
		return nil, err
	}
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	// Do not clone or type-assert the application's global DefaultTransport:
	// it may be replaced, typed nil, or carry a credential-intercepting dialer.
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		DisableCompression:    true,
	}
	u.Path = ""
	return &Client{origin: u.String(), merchantID: strconv.FormatInt(config.MerchantID, 10), private: priv, public: pub, now: time.Now,
		http: &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) signedValues(values url.Values) (url.Values, error) {
	if c == nil || c.private == nil || c.public == nil || c.now == nil {
		return nil, errors.New("goepay: client must be constructed with New")
	}
	fields, err := scalarValues(values)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"pid", "timestamp", "sign", "sign_type"} {
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("goepay: %s is filled by the SDK", key)
		}
	}
	fields["pid"], fields["timestamp"], fields["sign_type"] = c.merchantID, strconv.FormatInt(c.now().Unix(), 10), "RSA"
	fields["sign"], err = sign(fields, c.private)
	if err != nil {
		return nil, err
	}
	result := url.Values{}
	for k, v := range fields {
		result.Set(k, v)
	}
	return result, nil
}

// SubmitURL builds a signed browser payment URL offline. It does not open it,
// create an order, follow a redirect or perform a payment.
func (c *Client) SubmitURL(params url.Values) (string, error) {
	values, err := c.signedValues(params)
	if err != nil {
		return "", err
	}
	return c.origin + "/api/pay/submit?" + values.Encode(), nil
}

func (c *Client) Create(ctx context.Context, p url.Values) (*Response, error) {
	return c.call(ctx, "/api/pay/create", p)
}
func (c *Client) Query(ctx context.Context, p url.Values) (*Response, error) {
	return c.call(ctx, "/api/pay/query", p)
}
func (c *Client) Refund(ctx context.Context, p url.Values) (*Response, error) {
	return c.call(ctx, "/api/pay/refund", p)
}
func (c *Client) RefundQuery(ctx context.Context, p url.Values) (*Response, error) {
	return c.call(ctx, "/api/pay/refundquery", p)
}
func (c *Client) Close(ctx context.Context, p url.Values) (*Response, error) {
	return c.call(ctx, "/api/pay/close", p)
}

// call sends exactly one application request. No SDK retry or redirect is used;
// transport errors may leave the remote outcome unknown. Query the original ID.
func (c *Client) call(ctx context.Context, path string, params url.Values) (*Response, error) {
	values, err := c.signedValues(params)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin+path, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	request.GetBody = nil // no transport replay body for a financial POST
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "GoEPay-Go-SDK/1.0")
	reply, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer reply.Body.Close()
	if reply.StatusCode >= 300 && reply.StatusCode < 400 {
		return nil, fmt.Errorf("%w: redirects are not followed", ErrProtocol)
	}
	media, _, err := mime.ParseMediaType(reply.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || (reply.Header.Get("Content-Encoding") != "" && reply.Header.Get("Content-Encoding") != "identity") {
		return nil, ErrProtocol
	}
	body, err := io.ReadAll(io.LimitReader(reply.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxResponseBytes {
		return nil, fmt.Errorf("%w: response exceeds 1 MiB", ErrProtocol)
	}
	fields, code, err := decodeFields(body)
	if err != nil {
		return nil, err
	}
	result := &Response{Fields: fields, Code: code}
	_, hasSign := fields["sign"]
	_, hasType := fields["sign_type"]
	_, hasTime := fields["timestamp"]
	if hasSign || hasType || hasTime {
		if err := verify(fields, c.public, c.now()); err != nil {
			return nil, err
		}
		result.Authenticated = true
	} else if code == 0 {
		return nil, ErrSignature
	}
	if code != 0 || reply.StatusCode != http.StatusOK {
		return result, &APIError{Code: code, Message: fields["msg"], HTTPStatus: reply.StatusCode, Authenticated: result.Authenticated}
	}
	return result, nil
}

// VerifyNotification validates V2 RSA, freshness, unique scalar keys and pid.
// Pass every decoded query parameter, including parameters originally present
// in notify_url. Call url.ParseQuery explicitly and reject its error first.
// This does not check your order, amount, success status or fulfillment replay.
func (c *Client) VerifyNotification(params url.Values) (map[string]string, error) {
	if c == nil || c.public == nil || c.now == nil {
		return nil, errors.New("goepay: client must be constructed with New")
	}
	fields, err := scalarValues(params)
	if err != nil {
		return nil, err
	}
	if err := verify(fields, c.public, c.now()); err != nil {
		return nil, err
	}
	if fields["pid"] != c.merchantID {
		return nil, errors.New("goepay: notification belongs to another merchant")
	}
	return fields, nil
}

// CloseIdleConnections releases this client's idle HTTP connections. It does not
// cancel in-flight calls and is useful when a short-lived client is discarded.
func (c *Client) CloseIdleConnections() {
	if c != nil && c.http != nil {
		c.http.CloseIdleConnections()
	}
}
