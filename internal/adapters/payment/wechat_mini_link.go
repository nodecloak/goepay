package payment

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrWechatMiniLink = errors.New("mini-program identity URL Link unavailable")
var ErrWechatMiniLinkLifetime = errors.New("mini-program URL Link cannot fit original request lifetime")
var ErrWechatMiniLinkRejected = errors.New("mini-program identity URL Link request rejected")

// WechatMiniLinkUnknownError means a link POST may have reached WeChat. The
// caller must preserve its durable unknown barrier; never automatically resend
// this original request or treat a missing response as proof nothing happened.
type WechatMiniLinkUnknownError struct{}

func (*WechatMiniLinkUnknownError) Error() string {
	return "mini-program identity URL Link creation outcome unknown"
}
func (*WechatMiniLinkUnknownError) Unwrap() error { return ErrWechatMiniLink }

type WechatMiniLinkConfig struct {
	AppID        string           `json:"app_id"`
	AppSecret    string           `json:"-"`
	IdentityPath string           `json:"identity_path"`
	HTTPClient   *http.Client     `json:"-"`
	Now          func() time.Time `json:"-"`
}

// WechatMiniLinkRequest must come from the original durable identity intent,
// including its birth and capped deadline. Neither a redirect nor query text is
// accepted: the adapter builds only the opaque original request_id/state pair.
type WechatMiniLinkRequest struct {
	RequestID string    `json:"request_id"`
	State     string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	Deadline  time.Time `json:"deadline"`
}

// RequestedExpiresAt records only the Unix-second cutoff sent to WeChat. Its
// success response does not supply an independently verified link expiry. It
// must never be confused with the transient access token's expires_in value.
type WechatMiniLinkResult struct {
	Provider           string    `json:"provider"`
	AppID              string    `json:"app_id"`
	RequestID          string    `json:"request_id"`
	URLLink            string    `json:"url_link"`
	RequestedExpiresAt time.Time `json:"requested_expires_at"`
}

type MiniIdentityLinkProvider interface {
	ID() string
	ApplicationID() string
	GenerateIdentityLink(context.Context, WechatMiniLinkRequest) (WechatMiniLinkResult, error)
}

// This capability has no payment, user-code exchange, query or refresh methods.
type WechatMiniLink struct {
	appID, appSecret, identityPath string
	client                         *http.Client
	now                            func() time.Time
	timeout                        time.Duration
}

const wechatMiniLinkTimeout = 10 * time.Second
const wechatMiniLinkMinimumWindow = time.Minute + wechatMiniLinkTimeout
const wechatMiniLinkStableEndpoint = "https://api.weixin.qq.com/cgi-bin/stable_token"
const wechatMiniLinkGenerateEndpoint = "https://api.weixin.qq.com/wxa/generate_urllink"

// NewWechatMiniLink performs zero I/O. Nil HTTPClient uses an owned secure
// transport. For explicit TLS test/trust hooks, a supplied client must contain a
// standard *http.Transport; arbitrary RoundTrippers (including the platform
// guardedTransport) are not accepted. Only DialContext and root CAs are copied,
// so registered alternate protocols/proxies/custom TLS dial cannot bypass TLS.
func NewWechatMiniLink(c WechatMiniLinkConfig) (*WechatMiniLink, error) {
	if !wechatMiniLinkIdentifier(c.AppID, 128) || !wechatMiniLinkValue(c.AppSecret, 512) || !wechatMiniLinkIdentityPath(c.IdentityPath) {
		return nil, ErrInvalid
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "api.weixin.qq.com"}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 32 << 10, DisableKeepAlives: true}
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	timeout := wechatMiniLinkTimeout
	if c.HTTPClient != nil {
		if c.HTTPClient.Timeout < 0 {
			return nil, ErrInvalid
		}
		if c.HTTPClient.Timeout > 0 && c.HTTPClient.Timeout < timeout {
			timeout = c.HTTPClient.Timeout
		}
		if c.HTTPClient.Transport != nil {
			base, ok := c.HTTPClient.Transport.(*http.Transport)
			if !ok || base.DialTLS != nil || base.DialTLSContext != nil {
				return nil, ErrInvalid
			}
			if base.TLSClientConfig != nil {
				cfg := base.TLSClientConfig
				if cfg.InsecureSkipVerify || cfg.ServerName != "" && cfg.ServerName != "api.weixin.qq.com" {
					return nil, ErrInvalid
				}
				if cfg.RootCAs != nil {
					transport.TLSClientConfig.RootCAs = cfg.RootCAs.Clone()
				}
			}
			if base.DialContext != nil {
				transport.DialContext = base.DialContext
			}
		}
	}
	client := &http.Client{Transport: transport, Timeout: timeout, Jar: nil, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if c.Now == nil {
		c.Now = time.Now
	}
	return &WechatMiniLink{appID: c.AppID, appSecret: c.AppSecret, identityPath: c.IdentityPath, client: client, now: c.Now, timeout: timeout}, nil
}

func (w *WechatMiniLink) ID() string            { return "wxpay" }
func (w *WechatMiniLink) ApplicationID() string { return w.appID }

func wechatMiniLinkIdentifier(s string, limit int) bool {
	if len(s) < 1 || len(s) > limit {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func wechatMiniLinkValue(value string, limit int) bool {
	if !validOAuthValue(value, limit) {
		return false
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}
func wechatMiniLinkIdentityPath(path string) bool {
	if len(path) < 1 || len(path) > 256 || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if r != '.' && !wechatMiniLinkIdentifier(string(r), 1) {
				return false
			}
		}
	}
	return true
}
func wechatMiniLinkOpaque(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'f' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
func wechatMiniLinkDeadline(in WechatMiniLinkRequest, now time.Time) (time.Time, error) {
	if !wechatMiniLinkOpaque(in.RequestID) || !wechatMiniLinkOpaque(in.State) || in.CreatedAt.IsZero() || in.CreatedAt.After(now) || in.Deadline.IsZero() || in.Deadline.After(in.CreatedAt.Add(5*time.Minute)) {
		return time.Time{}, ErrInvalid
	}
	requested := time.Unix(in.Deadline.Unix(), 0).UTC()
	if requested.Sub(now) <= wechatMiniLinkMinimumWindow {
		return time.Time{}, ErrWechatMiniLinkLifetime
	}
	return requested, nil
}

// The request body is nonempty and GetBody is deliberately absent. This makes
// transport retries unable to replay either credential or link-creation POST.
func wechatMiniLinkPostRequest(ctx context.Context, target string, body any) (*http.Request, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, ErrWechatMiniLink
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return nil, ErrWechatMiniLink
	}
	req.GetBody = nil
	req.Close = true
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}
func (w *WechatMiniLink) post(ctx context.Context, target string, body any) (map[string]json.RawMessage, error) {
	req, err := wechatMiniLinkPostRequest(ctx, target, body)
	if err != nil {
		return nil, ErrWechatMiniLink
	}
	response, err := w.client.Do(req)
	if err != nil {
		return nil, ErrWechatMiniLink
	}
	raw, err := oauthBody(response)
	if err != nil || !utf8.Valid(raw) || providerUniqueJSON(raw) != nil {
		return nil, ErrWechatMiniLink
	}
	out, err := oauthObject(raw)
	if err != nil {
		return nil, ErrWechatMiniLink
	}
	return out, nil
}
func wechatMiniLinkFields(out map[string]json.RawMessage, allowed ...string) bool {
	for key := range out {
		found := false
		for _, name := range allowed {
			if key == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if raw, present := out["errmsg"]; present {
		var message string
		if json.Unmarshal(raw, &message) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || len(message) > 1024 {
			return false
		}
	}
	return true
}
func wechatMiniLinkErrorCode(out map[string]json.RawMessage, required bool) (int64, bool) {
	raw, present := out["errcode"]
	if !present {
		return 0, !required
	}
	var code int64
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &code) != nil {
		return 0, false
	}
	return code, true
}
func wechatMiniLinkTokenExpiry(raw json.RawMessage) bool {
	var seconds int64
	return len(raw) > 0 && json.Unmarshal(raw, &seconds) == nil && seconds > 0 && seconds <= 7200
}

func (w *WechatMiniLink) GenerateIdentityLink(ctx context.Context, in WechatMiniLinkRequest) (WechatMiniLinkResult, error) {
	empty := WechatMiniLinkResult{}
	requested, err := wechatMiniLinkDeadline(in, w.now())
	if err != nil {
		return empty, err
	}
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()
	tokenResponse, err := w.post(ctx, wechatMiniLinkStableEndpoint, map[string]any{"grant_type": "client_credential", "appid": w.appID, "secret": w.appSecret, "force_refresh": false})
	if err != nil || !wechatMiniLinkFields(tokenResponse, "access_token", "expires_in", "errcode", "errmsg") {
		return empty, ErrWechatMiniLink
	}
	code, ok := wechatMiniLinkErrorCode(tokenResponse, false)
	if !ok {
		return empty, ErrWechatMiniLink
	}
	if code != 0 {
		return empty, ErrWechatMiniLinkRejected
	}
	token := text(tokenResponse, "access_token")
	if !wechatMiniLinkValue(token, 4096) || !wechatMiniLinkTokenExpiry(tokenResponse["expires_in"]) {
		return empty, ErrWechatMiniLink
	}
	if _, err = wechatMiniLinkDeadline(in, w.now()); err != nil {
		return empty, err
	}
	if ctx.Err() != nil {
		return empty, ErrWechatMiniLink
	}
	query := url.Values{"request_id": {in.RequestID}, "state": {in.State}}.Encode()
	linkResponse, err := w.post(ctx, wechatMiniLinkGenerateEndpoint+"?"+url.Values{"access_token": {token}}.Encode(), map[string]any{"path": w.identityPath, "query": query, "expire_type": 0, "expire_time": requested.Unix(), "env_version": "release"})
	if err != nil || !wechatMiniLinkFields(linkResponse, "errcode", "errmsg", "url_link") {
		return empty, &WechatMiniLinkUnknownError{}
	}
	code, ok = wechatMiniLinkErrorCode(linkResponse, true)
	if !ok {
		return empty, &WechatMiniLinkUnknownError{}
	}
	if code != 0 {
		return empty, ErrWechatMiniLinkRejected
	}
	link := text(linkResponse, "url_link")
	if !wechatMiniLinkVendorURL(link) || ctx.Err() != nil || !w.now().Before(requested) {
		return empty, &WechatMiniLinkUnknownError{}
	}
	return WechatMiniLinkResult{Provider: w.ID(), AppID: w.appID, RequestID: in.RequestID, URLLink: link, RequestedExpiresAt: requested}, nil
}

func wechatMiniLinkVendorURL(raw string) bool {
	if len(raw) < 1 || len(raw) > 2048 || !strings.HasPrefix(raw, "https://") || strings.ContainsAny(raw, "\\#") {
		return false
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.ForceQuery || u.Host != "wxaurl.cn" && u.Host != "wxmpurl.cn" || len(u.Path) < 2 || len(u.Path) > 1024 || !strings.HasPrefix(u.Path, "/") || strings.Trim(u.Path, "/") == "" {
		return false
	}
	for _, r := range u.Path {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '\\' {
			return false
		}
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	if u.RawQuery != "" {
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(query) != 1 || len(query["cq"]) != 1 {
			return false
		}
		value := query.Get("cq")
		if len(value) < 1 || len(value) > 256 {
			return false
		}
		for _, r := range value {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$&'()*+,/:;=?@-._~%`", r)) {
				return false
			}
		}
	}
	return true
}

var _ MiniIdentityLinkProvider = (*WechatMiniLink)(nil)
