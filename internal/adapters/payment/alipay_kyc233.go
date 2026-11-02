package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// KYC233 methods reuse the AOP request signer and exact response/certificate
// verifier. They use the fixed production endpoint independently of payment
// gateway overrides and never expose upstream errors or identity payloads.
func (a *Alipay) kyc233Call(ctx context.Context, method string, biz map[string]any, top map[string]string, token bool) (map[string]json.RawMessage, error) {
	if a.validateCertificates() != nil {
		return nil, ErrAuthentication
	}
	p, e := a.params(method, biz)
	if e != nil {
		return nil, ErrAuthentication
	}
	if biz == nil {
		delete(p, "biz_content")
	}
	for k, v := range top {
		p[k] = v
	}
	p["sign"], e = SignAlipayRSA2(p, a.config.PrivateKey)
	if e != nil {
		return nil, ErrAuthentication
	}
	form := url.Values{}
	for k, v := range p {
		form.Set(k, v)
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, "https://openapi.alipay.com/gateway.do", strings.NewReader(form.Encode()))
	if e != nil {
		return nil, ErrAuthentication
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.GetBody = nil
	req.Close = true
	res, e := oauthClient(a.config.HTTPClient).Do(req)
	if e != nil {
		return nil, ErrAuthentication
	}
	// The pinned signing key authenticates the payload; verified HTTPS also
	// prevents leaking credentials/identity to an unauthenticated transport.
	if res.TLS == nil || !res.TLS.HandshakeComplete || len(res.TLS.VerifiedChains) == 0 {
		res.Body.Close()
		return nil, ErrAuthentication
	}
	raw, e := oauthBody(res)
	if e != nil || providerUniqueJSON(raw) != nil {
		return nil, ErrAuthentication
	}
	envelope, e := oauthObject(raw)
	if e != nil {
		return nil, ErrAuthentication
	}
	node := envelope[strings.ReplaceAll(method, ".", "_")+"_response"]
	if len(node) == 0 || !a.verifyResponse(node, text(envelope, "sign"), envelope) {
		return nil, ErrAuthentication
	}
	out, e := oauthObject(node)
	if e != nil {
		return nil, ErrAuthentication
	}
	if code := text(out, "code"); code != "10000" && !(token && code == "") {
		return nil, ErrAuthentication
	}
	if app := text(out, "app_id"); app != "" && app != a.ApplicationID() {
		return nil, ErrAuthentication
	}
	return out, nil
}
func kyc233Text(s string, n int) bool { return validOAuthValue(s, n) && !strings.HasPrefix(s, "@") }
func kyc233Callback(s string) bool {
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && u.Opaque == "" && len(s) <= 2048 && !strings.ContainsAny(s, "\\\r\n\x00")
}

func (a *Alipay) KYC233Initialize(ctx context.Context, request, name, number string, card int, redirect string) (string, string, error) {
	cards := []string{"IDENTITY_CARD", "HOME_VISIT_PERMIT_HK_MC", "HOME_VISIT_PERMIT_TAIWAN", "RESIDENCE_PERMIT_HK_MC", "RESIDENCE_PERMIT_TAIWAN", "PERMANENT_RESIDENCE_FOREIGNER"}
	if !kyc233Text(request, 32) || !kyc233Text(name, 128) || !kyc233Text(number, 128) || card < 0 || card >= len(cards) || !kyc233Callback(redirect) {
		return "", "", ErrInvalid
	}
	out, e := a.kyc233Call(ctx, "alipay.user.certify.open.initialize", map[string]any{"outer_order_no": request, "biz_code": "SMART_FACE", "identity_param": map[string]string{"identity_type": "CERT_INFO", "cert_type": cards[card], "cert_name": name, "cert_no": number}, "merchant_config": map[string]string{"return_url": redirect}}, nil, false)
	if e != nil {
		return "", "", e
	}
	id := text(out, "certify_id")
	if !kyc233Text(id, 4096) {
		return "", "", ErrAuthentication
	}
	p, e := a.params("alipay.user.certify.open.certify", map[string]any{"certify_id": id})
	if e != nil {
		return "", "", ErrAuthentication
	}
	p["sign"], e = SignAlipayRSA2(p, a.config.PrivateKey)
	if e != nil {
		return "", "", ErrAuthentication
	}
	q := url.Values{}
	for k, v := range p {
		q.Set(k, v)
	}
	return id, "https://openapi.alipay.com/gateway.do?" + q.Encode(), nil
}
func (a *Alipay) KYC233Query(ctx context.Context, id string) (string, error) {
	if !kyc233Text(id, 4096) {
		return "", ErrInvalid
	}
	out, e := a.kyc233Call(ctx, "alipay.user.certify.open.query", map[string]any{"certify_id": id}, nil, false)
	if e != nil {
		return "", e
	}
	switch text(out, "passed") {
	case "T":
		return "passed", nil
	case "F":
		return "pending", nil
	default:
		return "", ErrAuthentication
	}
}
func (a *Alipay) KYC233Preconsult(ctx context.Context, name, number, state, redirect string) (string, string, error) {
	if !kyc233Text(name, 128) || !kyc233Text(number, 128) || !validApplicationAuthorizationValue(state, 128) || len(state) < 32 || !kyc233Callback(redirect) {
		return "", "", ErrInvalid
	}
	out, e := a.kyc233Call(ctx, "alipay.user.certdoc.certverify.preconsult", map[string]any{"user_name": name, "cert_type": "IDENTITY_CARD", "cert_no": number}, nil, false)
	if e != nil {
		return "", "", e
	}
	id := text(out, "verify_id")
	if !kyc233Text(id, 4096) {
		return "", "", ErrAuthentication
	}
	q := url.Values{"app_id": {a.ApplicationID()}, "scope": {"id_verify"}, "redirect_uri": {redirect}, "cert_verify_id": {id}, "state": {state}}
	return id, "https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?" + q.Encode(), nil
}

// The caller must durably consume the bound one-use authorization-code claim.
func (a *Alipay) KYC233Consult(ctx context.Context, id, code string) (string, error) {
	if !kyc233Text(id, 4096) || !validApplicationAuthorizationValue(code, 4096) {
		return "", ErrInvalid
	}
	token, e := a.kyc233Call(ctx, "alipay.system.oauth.token", nil, map[string]string{"grant_type": "authorization_code", "code": code}, true)
	if e != nil {
		return "", e
	}
	access := text(token, "access_token")
	if !validApplicationAuthorizationValue(access, 4096) || !oauthPositive(token["expires_in"]) {
		return "", ErrAuthentication
	}
	out, e := a.kyc233Call(ctx, "alipay.user.certdoc.certverify.consult", map[string]any{"verify_id": id}, map[string]string{"auth_token": access}, false)
	if e != nil {
		return "", e
	}
	switch text(out, "passed") {
	case "T":
		return "passed", nil
	case "F":
		return "rejected", nil
	default:
		return "", ErrAuthentication
	}
}
