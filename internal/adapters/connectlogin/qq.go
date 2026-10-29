// Package connectlogin ports the merchant quick-login OAuth providers from
// epay_release (user/connect.php, includes/lib/QC.php, Oauth.php). Each provider
// exposes an authorize URL (carrying an anti-CSRF state the caller stores) and a
// callback exchange that returns the provider's stable user id (openid/uid).
// An unknown identity is never logged in by this package — it only resolves the
// id; the server decides login vs bind. Base URLs are injectable so httptest
// exercises every upstream call without reaching a real provider.
package connectlogin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

var (
	ErrConfig   = errors.New("quick-login provider not configured")
	ErrUpstream = errors.New("quick-login upstream rejected or unconfirmed")
)

func client(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// Provider is the unified quick-login contract the server handler drives. Both
// QQ (direct graph.qq.com) and OauthRelay (self-hosted wx/alipay relay) satisfy
// it. AuthorizeURL may perform I/O (the relay does), so it takes a context.
type Provider interface {
	AuthorizeURL(ctx context.Context, state string) (string, error)
	Exchange(ctx context.Context, code string) (string, error)
}

// QQ is Tencent QQ Connect (graph.qq.com). AuthBase/TokenBase default to the
// real endpoints; tests point them at an httptest server.
type QQ struct {
	AppID       string
	AppKey      string
	CallbackURL string
	AuthBase    string // default https://graph.qq.com/oauth2.0/authorize
	TokenBase   string // default https://graph.qq.com/oauth2.0/token
	Client      *http.Client
}

// AuthorizeURL builds the QQ authorize redirect carrying state. The caller must
// persist state and compare it on callback (CSRF defence). ctx is unused (QQ
// builds the URL locally) but present to satisfy the Provider interface.
func (q QQ) AuthorizeURL(ctx context.Context, state string) (string, error) {
	_ = ctx
	if q.AppID == "" || q.CallbackURL == "" || state == "" {
		return "", ErrConfig
	}
	base := q.AuthBase
	if base == "" {
		base = "https://graph.qq.com/oauth2.0/authorize"
	}
	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", q.AppID)
	v.Set("redirect_uri", q.CallbackURL)
	v.Set("state", state)
	return base + "?" + v.Encode(), nil
}

// Exchange trades the callback code for the QQ openid (the stable user id).
// It uses fmt=json&need_openid=1, so the token response already carries openid,
// matching QC::qq_callback. Returns ErrUpstream on any non-OK / unparseable
// reply so an unconfirmed outcome is never treated as a real identity.
func (q QQ) Exchange(ctx context.Context, code string) (string, error) {
	if q.AppID == "" || q.AppKey == "" || q.CallbackURL == "" {
		return "", ErrConfig
	}
	if code == "" {
		return "", ErrUpstream
	}
	base := q.TokenBase
	if base == "" {
		base = "https://graph.qq.com/oauth2.0/token"
	}
	v := url.Values{}
	v.Set("grant_type", "authorization_code")
	v.Set("client_id", q.AppID)
	v.Set("client_secret", q.AppKey)
	v.Set("code", code)
	v.Set("redirect_uri", q.CallbackURL)
	v.Set("fmt", "json")
	v.Set("need_openid", "1")
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+v.Encode(), nil)
	if e != nil {
		return "", ErrUpstream
	}
	resp, e := client(q.Client).Do(req)
	if e != nil {
		return "", ErrUpstream
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if e != nil || resp.StatusCode != 200 {
		return "", ErrUpstream
	}
	var out struct {
		AccessToken string `json:"access_token"`
		OpenID      string `json:"openid"`
		Error       string `json:"error"`
	}
	if json.Unmarshal(body, &out) != nil || out.Error != "" || out.OpenID == "" {
		return "", ErrUpstream
	}
	return out.OpenID, nil
}
