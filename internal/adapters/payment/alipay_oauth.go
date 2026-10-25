package payment

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

func (a *Alipay) OAuthAuthorizationURL(state, redirect string) (string, error) {
	if e := a.validateCertificates(); e != nil {
		return "", ErrOAuthExchange
	}
	return oauthAuthorizationURL(a.ID(), a.ApplicationID(), state, redirect)
}

// OAuth differs from trade APIs: grant_type/code are top-level signed fields
// and successful token nodes may omit code=10000. The same RSA2/certificate
// verifier still authenticates the exact returned token node.
func (a *Alipay) ExchangeOAuth(ctx context.Context, code string) (OAuthIdentity, error) {
	if !validOAuthValue(code, 4096) {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	params, e := a.params("alipay.system.oauth.token", nil)
	if e != nil {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	delete(params, "biz_content")
	params["grant_type"] = "authorization_code"
	params["code"] = code
	params["sign"], e = SignAlipayRSA2(params, a.config.PrivateKey)
	if e != nil {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	req, e := http.NewRequestWithContext(ctx, "POST", "https://openapi.alipay.com/gateway.do", strings.NewReader(form.Encode()))
	if e != nil {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	res, e := oauthClient(a.config.HTTPClient).Do(req)
	if e != nil {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	raw, e := oauthBody(res)
	if e != nil {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	envelope, e := oauthObject(raw)
	if e != nil {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	node := envelope["alipay_system_oauth_token_response"]
	if len(node) == 0 || !a.verifyResponse(node, text(envelope, "sign"), envelope) {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	out, e := oauthObject(node)
	if e != nil {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	if resultCode := text(out, "code"); resultCode != "" && resultCode != "10000" {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	if !validOAuthValue(text(out, "access_token"), 4096) || !oauthPositive(out["expires_in"]) {
		return OAuthIdentity{}, ErrOAuthExchange
	}
	subject := text(out, "user_id")
	if subject != "" {
		if len(subject) != 16 || !strings.HasPrefix(subject, "2088") || !onlyDigits(subject) {
			return OAuthIdentity{}, ErrOAuthExchange
		}
	} else {
		subject = text(out, "open_id")
		if !validOAuthValue(subject, 128) || strings.HasPrefix(subject, "2088") && onlyDigits(subject) {
			return OAuthIdentity{}, ErrOAuthExchange
		}
	}
	return OAuthIdentity{Provider: a.ID(), AppID: a.ApplicationID(), SubjectID: subject}, nil
}

var _ WebOAuthProvider = (*Alipay)(nil)
var _ WebOAuthProvider = (*WechatOAuth)(nil)
