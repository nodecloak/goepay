package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// MiniOAuthProvider exchanges a wallet SDK's one-use code for an application
// bound payer. Session keys and access tokens never leave this boundary.
type MiniOAuthProvider interface {
	ID() string
	ApplicationID() string
	ExchangeMiniCode(context.Context, string) (OAuthIdentity, error)
}
type WechatMiniOAuth struct{ config WechatOAuthConfig }

func NewWechatMiniOAuth(c WechatOAuthConfig) (*WechatMiniOAuth, error) {
	if !validOAuthValue(c.AppID, 128) || !validOAuthValue(c.AppSecret, 512) {
		return nil, ErrInvalid
	}
	c.HTTPClient = oauthClient(c.HTTPClient)
	return &WechatMiniOAuth{c}, nil
}
func (w *WechatMiniOAuth) ID() string            { return "wxpay" }
func (w *WechatMiniOAuth) ApplicationID() string { return w.config.AppID }
func (w *WechatMiniOAuth) ExchangeMiniCode(ctx context.Context, code string) (OAuthIdentity, error) {
	if !validOAuthValue(code, 4096) {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	q := url.Values{"appid": {w.config.AppID}, "secret": {w.config.AppSecret}, "js_code": {code}, "grant_type": {"authorization_code"}}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://api.weixin.qq.com/sns/jscode2session?"+q.Encode(), nil)
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
	if !validOAuthValue(subject, 128) || !validOAuthValue(text(node, "session_key"), 4096) || text(node, "appid") != "" && text(node, "appid") != w.config.AppID {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	return OAuthIdentity{Provider: w.ID(), AppID: w.ApplicationID(), SubjectID: subject}, nil
}
func (a *Alipay) ExchangeMiniCode(ctx context.Context, code string) (OAuthIdentity, error) {
	return a.ExchangeOAuth(ctx, code)
}

var _ MiniOAuthProvider = (*WechatMiniOAuth)(nil)
var _ MiniOAuthProvider = (*Alipay)(nil)
