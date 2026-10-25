package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrOAuthExchange = errors.New("OAuth identity exchange failed")

type OAuthIdentity struct{ Provider, AppID, SubjectID string }
type WebOAuthProvider interface {
	ID() string
	ApplicationID() string
	OAuthAuthorizationURL(state, redirect string) (string, error)
	ExchangeOAuth(context.Context, string) (OAuthIdentity, error)
}

func validOAuthValue(s string, limit int) bool {
	return s != "" && len(s) <= limit && strings.TrimSpace(s) == s && !strings.HasPrefix(s, "@") && !strings.ContainsAny(s, "\r\n\x00")
}
func validOAuthRedirect(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && u.RawQuery == "" && u.Opaque == ""
}
func oauthAuthorizationURL(provider, app, state, redirect string) (string, error) {
	if len(state) < 32 || len(state) > 128 || !validOAuthRedirect(redirect) {
		return "", ErrInvalid
	}
	for _, r := range state {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", ErrInvalid
		}
	}
	p := url.Values{"state": {state}, "redirect_uri": {redirect}}
	if provider == "wxpay" {
		p.Set("appid", app)
		p.Set("response_type", "code")
		p.Set("scope", "snsapi_base")
		return "https://open.weixin.qq.com/connect/oauth2/authorize?" + p.Encode() + "#wechat_redirect", nil
	}
	if provider == "alipay" {
		p.Set("app_id", app)
		p.Set("scope", "auth_base")
		return "https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?" + p.Encode(), nil
	}
	return "", ErrUnsupported
}
func oauthClient(c *http.Client) *http.Client {
	out := http.Client{}
	if c != nil {
		out = *c
	}
	if out.Timeout <= 0 || out.Timeout > 10*time.Second {
		out.Timeout = 10 * time.Second
	}
	out.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	out.Jar = nil
	return &out
}

// Exact-object parsing rejects duplicate JSON members instead of allowing a
// second identity or signature field to silently overwrite the first.
func oauthObject(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return nil, ErrOAuthExchange
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		key, e := d.Token()
		name, ok := key.(string)
		if e != nil || !ok {
			return nil, ErrOAuthExchange
		}
		if _, exists := out[name]; exists {
			return nil, ErrOAuthExchange
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, ErrOAuthExchange
		}
		out[name] = value
	}
	if _, e = d.Token(); e != nil {
		return nil, ErrOAuthExchange
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrOAuthExchange
	}
	return out, nil
}
func oauthPositive(raw json.RawMessage) bool {
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n > 0
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	n, e := strconv.ParseInt(s, 10, 64)
	return e == nil && n > 0
}
func oauthBody(res *http.Response) ([]byte, error) {
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, 64<<10+1))
	if e != nil || res.StatusCode != 200 || len(raw) > 64<<10 {
		return nil, ErrOAuthExchange
	}
	return raw, nil
}

type WechatOAuthConfig struct {
	AppID, AppSecret string
	HTTPClient       *http.Client
}
type WechatOAuth struct{ config WechatOAuthConfig }

func NewWechatOAuth(c WechatOAuthConfig) (*WechatOAuth, error) {
	if !validOAuthValue(c.AppID, 128) || !validOAuthValue(c.AppSecret, 512) {
		return nil, ErrInvalid
	}
	c.HTTPClient = oauthClient(c.HTTPClient)
	return &WechatOAuth{c}, nil
}
func (w *WechatOAuth) ID() string            { return "wxpay" }
func (w *WechatOAuth) ApplicationID() string { return w.config.AppID }
func (w *WechatOAuth) OAuthAuthorizationURL(state, redirect string) (string, error) {
	return oauthAuthorizationURL(w.ID(), w.ApplicationID(), state, redirect)
}
func (w *WechatOAuth) ExchangeOAuth(ctx context.Context, code string) (OAuthIdentity, error) {
	if !validOAuthValue(code, 4096) {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	query := url.Values{"appid": {w.config.AppID}, "secret": {w.config.AppSecret}, "code": {code}, "grant_type": {"authorization_code"}}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://api.weixin.qq.com/sns/oauth2/access_token?"+query.Encode(), nil)
	if e != nil {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	res, e := w.config.HTTPClient.Do(req)
	if e != nil {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	raw, e := oauthBody(res)
	if e != nil {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	node, e := oauthObject(raw)
	if e != nil {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	if errorCode, ok := node["errcode"]; ok {
		var n int
		if json.Unmarshal(errorCode, &n) != nil || n != 0 {
			return OAuthIdentity{}, ErrOAuthExchange
		}
	}
	subject := text(node, "openid")
	if !validOAuthValue(subject, 128) || !validOAuthValue(text(node, "access_token"), 4096) || !oauthPositive(node["expires_in"]) || text(node, "scope") != "snsapi_base" || text(node, "appid") != "" && text(node, "appid") != w.config.AppID {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	return OAuthIdentity{Provider: w.ID(), AppID: w.ApplicationID(), SubjectID: subject}, nil
}
