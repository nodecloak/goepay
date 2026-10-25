package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const alipayApplicationAuthorizationGateway = "https://openapi.alipay.com/gateway.do"

// AlipayApplicationAuthorization deliberately does not embed Alipay, so payment
// methods and user OAuth exchange cannot be invoked through this capability.
type AlipayApplicationAuthorization struct{ client *Alipay }

// NewAlipayApplicationAuthorization reuses only the trusted signing/transport
// configuration. A custom gateway is never accepted, even for authorization.
func NewAlipayApplicationAuthorization(c AlipayConfig) (*AlipayApplicationAuthorization, error) {
	if !validAlipayAuthorizationAppID(c.AppID) || c.GatewayURL != "" && c.GatewayURL != alipayApplicationAuthorizationGateway {
		return nil, ErrInvalid
	}
	client, err := NewAlipay(AlipayConfig{AppID: c.AppID, PrivateKey: c.PrivateKey, PublicKey: c.PublicKey, CertificateMode: c.CertificateMode, Now: c.Now, HTTPClient: oauthClient(c.HTTPClient), GatewayURL: alipayApplicationAuthorizationGateway})
	if err != nil {
		return nil, ErrInvalid
	}
	return &AlipayApplicationAuthorization{client: client}, nil
}

func (a *AlipayApplicationAuthorization) ID() string            { return "alipay" }
func (a *AlipayApplicationAuthorization) ApplicationID() string { return a.client.ApplicationID() }

func validAlipayAuthorizationAppID(app string) bool { return len(app) == 16 && onlyDigits(app) }

func validApplicationAuthorizationValue(value string, limit int) bool {
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

func validApplicationAuthorizationLaunch(state, redirect string) bool {
	if len(state) < 32 || len(state) > 128 || len(redirect) > 2048 || !validOAuthRedirect(redirect) || strings.ContainsAny(redirect, "\\?# \t\r\n\x00") {
		return false
	}
	for _, r := range state {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	u, err := url.Parse(redirect)
	if err != nil || u.Hostname() == "" {
		return false
	}
	for _, r := range u.Path {
		if r < 32 || r == 127 || r == '\\' {
			return false
		}
	}
	return true
}

func (a *AlipayApplicationAuthorization) ApplicationAuthorizationURL(state, redirect string) (string, error) {
	if !validApplicationAuthorizationLaunch(state, redirect) {
		return "", ErrInvalid
	}
	if a.client.validateCertificates() != nil {
		return "", ErrApplicationAuthorization
	}
	query := url.Values{"app_id": {a.ApplicationID()}, "redirect_uri": {redirect}, "state": {state}}
	return "https://openauth.alipay.com/oauth2/appToAppAuth.htm?" + query.Encode(), nil
}

// AssignedApplicationAuthorizationURLs follows appOauthAssign in the bundled
// SDK, including its single encoding of the wallet page prefix and bizData.
func (a *AlipayApplicationAuthorization) AssignedApplicationAuthorizationURLs(state, redirect string, appTypes []AlipayAuthorizationAppType) (AssignedApplicationAuthorizationLaunch, error) {
	if !validApplicationAuthorizationLaunch(state, redirect) || len(appTypes) == 0 || len(appTypes) > 5 {
		return AssignedApplicationAuthorizationLaunch{}, ErrInvalid
	}
	seen := map[AlipayAuthorizationAppType]bool{}
	for _, kind := range appTypes {
		switch kind {
		case AlipayAuthorizationMobileApp, AlipayAuthorizationWebApp, AlipayAuthorizationPublicApp, AlipayAuthorizationTinyApp, AlipayAuthorizationBaseApp:
		default:
			return AssignedApplicationAuthorizationLaunch{}, ErrInvalid
		}
		if seen[kind] {
			return AssignedApplicationAuthorizationLaunch{}, ErrInvalid
		}
		seen[kind] = true
	}
	if a.client.validateCertificates() != nil {
		return AssignedApplicationAuthorizationLaunch{}, ErrApplicationAuthorization
	}
	// Struct order mirrors the bundled SDK and avoids additional protocol keys.
	data := struct {
		PlatformCode string `json:"platformCode"`
		TaskType     string `json:"taskType"`
		AgentOpParam struct {
			RedirectURI string                       `json:"redirectUri"`
			AppTypes    []AlipayAuthorizationAppType `json:"appTypes"`
			ISVAppID    string                       `json:"isvAppId"`
			State       string                       `json:"state"`
		} `json:"agentOpParam"`
	}{PlatformCode: "O", TaskType: "INTERFACE_AUTH"}
	data.AgentOpParam.RedirectURI = redirect
	data.AgentOpParam.AppTypes = appTypes
	data.AgentOpParam.ISVAppID = a.ApplicationID()
	data.AgentOpParam.State = state
	raw, err := json.Marshal(data)
	if err != nil {
		return AssignedApplicationAuthorizationLaunch{}, ErrInvalid
	}
	// PHP rawurlencode uses %20 instead of QueryEscape's + for spaces.
	encoded := strings.ReplaceAll(url.QueryEscape(string(raw)), "+", "%20")
	return AssignedApplicationAuthorizationLaunch{PCURL: "https://b.alipay.com/page/message/tasksDetail?bizData=" + encoded, WalletURL: "alipays://platformapi/startapp?appId=2021003130652097&page=pages%2Fauthorize%2Findex%3FbizData%3D" + encoded}, nil
}

func (a *AlipayApplicationAuthorization) ExchangeApplicationAuthorization(ctx context.Context, in ApplicationAuthorizationExchange) (ApplicationAuthorizationResult, error) {
	failed := func() (ApplicationAuthorizationResult, error) {
		return ApplicationAuthorizationResult{}, ErrApplicationAuthorization
	}
	if !validApplicationAuthorizationValue(in.Code, 4096) || !validAlipayAuthorizationAppID(in.ExpectedAuthAppID) || a.client.validateCertificates() != nil {
		return failed()
	}
	params, err := a.client.params("alipay.open.auth.token.app", map[string]any{"grant_type": "authorization_code", "code": in.Code})
	if err != nil {
		return failed()
	}
	params["sign"], err = SignAlipayRSA2(params, a.client.config.PrivateKey)
	if err != nil {
		return failed()
	}
	form := url.Values{}
	for key, value := range params {
		form.Set(key, value)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, alipayApplicationAuthorizationGateway, strings.NewReader(form.Encode()))
	if err != nil {
		return failed()
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	// The original code claim permits one transport attempt, including when a
	// cached connection fails before writing. Never make this POST replayable.
	req.GetBody = nil
	res, err := oauthClient(a.client.config.HTTPClient).Do(req)
	if err != nil {
		return failed()
	}
	raw, err := oauthBody(res)
	if err != nil || providerUniqueJSON(raw) != nil {
		return failed()
	}
	envelope, err := oauthObject(raw)
	if err != nil {
		return failed()
	}
	node := envelope["alipay_open_auth_token_app_response"]
	if len(node) == 0 || !a.client.verifyResponse(node, text(envelope, "sign"), envelope) {
		return failed()
	}
	out, err := oauthObject(node)
	if err != nil || text(out, "code") != "10000" {
		return failed()
	}
	authApp, user, token := text(out, "auth_app_id"), text(out, "user_id"), text(out, "app_auth_token")
	if authApp != in.ExpectedAuthAppID || !validAlipayAuthorizationAppID(authApp) || len(user) != 16 || !strings.HasPrefix(user, "2088") || !onlyDigits(user) || !validApplicationAuthorizationValue(token, 4096) {
		return failed()
	}
	if _, ok := out["app_id"]; ok && text(out, "app_id") != a.ApplicationID() {
		return failed()
	}
	var expires int64
	if value, present := out["expires_in"]; present {
		expires, err = applicationAuthorizationLifetime(value)
		if err != nil {
			return failed()
		}
	}
	if value, present := out["re_expires_in"]; present {
		if _, err = applicationAuthorizationLifetime(value); err != nil {
			return failed()
		}
	}
	return ApplicationAuthorizationResult{Provider: a.ID(), IssuerAppID: a.ApplicationID(), AuthAppID: authApp, UserID: user, AppAuthToken: token, TokenExpiresInSeconds: expires}, nil
}

func applicationAuthorizationLifetime(raw json.RawMessage) (int64, error) {
	var n int64
	if json.Unmarshal(raw, &n) != nil {
		var value string
		if json.Unmarshal(raw, &value) != nil || value == "" || !onlyDigits(value) {
			return 0, ErrApplicationAuthorization
		}
		var err error
		n, err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, ErrApplicationAuthorization
		}
	}
	if n <= 0 {
		return 0, ErrApplicationAuthorization
	}
	return n, nil
}

var _ ApplicationAuthorizationProvider = (*AlipayApplicationAuthorization)(nil)
