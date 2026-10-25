package payment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const appAuthorizationIssuer = "2021000000000001"
const appAuthorizationTarget = "2021000000000002"
const appAuthorizationResponse = `{"code":"10000","msg":"Success","auth_app_id":"2021000000000002","user_id":"2088000000000000","app_auth_token":"synthetic-app-token","app_refresh_token":"discard-refresh","re_expires_in":"31536000"}`

// These cases catch mixing user OAuth and application authorization protocols,
// arbitrary launch destinations, and incorrect assigned-app URL encoding.
func TestApplicationAuthorizationLaunchMatchesLocalSDK(t *testing.T) {
	key := testKey(t)
	p, err := NewAlipayApplicationAuthorization(AlipayConfig{AppID: appAuthorizationIssuer, PrivateKey: key, PublicKey: &key.PublicKey})
	if err != nil {
		t.Fatal(err)
	}
	state, redirect := strings.Repeat("a", 64), "https://pay.example.invalid/app-authorizations/callback/"
	raw, err := p.ApplicationAuthorizationURL(state, redirect)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "openauth.alipay.com" || u.Path != "/oauth2/appToAppAuth.htm" || len(u.Query()) != 3 || u.Query().Get("app_id") != appAuthorizationIssuer || u.Query().Get("redirect_uri") != redirect || u.Query().Get("state") != state {
		t.Fatal("wrong application authorization launch")
	}
	launch, err := p.AssignedApplicationAuthorizationURLs(state, redirect, []AlipayAuthorizationAppType{AlipayAuthorizationWebApp, AlipayAuthorizationTinyApp})
	if err != nil {
		t.Fatal(err)
	}
	pc, _ := url.Parse(launch.PCURL)
	wallet, _ := url.Parse(launch.WalletURL)
	want := `{"platformCode":"O","taskType":"INTERFACE_AUTH","agentOpParam":{"redirectUri":"https://pay.example.invalid/app-authorizations/callback/","appTypes":["WEBAPP","TINYAPP"],"isvAppId":"2021000000000001","state":"` + state + `"}}`
	if pc.Scheme != "https" || pc.Host != "b.alipay.com" || pc.Path != "/page/message/tasksDetail" || len(pc.Query()) != 1 || pc.Query().Get("bizData") != want {
		t.Fatal("wrong assigned PC launch")
	}
	// The SDK encodes the fixed page prefix and bizData once, not twice.
	if wallet.Scheme != "alipays" || wallet.Host != "platformapi" || wallet.Path != "/startapp" || len(wallet.Query()) != 2 || wallet.Query().Get("appId") != "2021003130652097" || wallet.Query().Get("page") != "pages/authorize/index?bizData="+want {
		t.Fatal("wrong assigned wallet launch")
	}
	for _, bad := range []string{"http://pay.example.invalid/cb", "https://user@pay.example.invalid/cb", "https://pay.example.invalid/cb?q=1", "https://pay.example.invalid/cb#x", "https://pay.example.invalid/cb\n", "https://pay.example.invalid/cb?", "https://pay.example.invalid/cb#", "https://pay.example.invalid/cb%5cnext", "https://pay.example.invalid/cb%00"} {
		if _, err := p.ApplicationAuthorizationURL(state, bad); err == nil {
			t.Fatal("invalid callback accepted")
		}
	}
	for _, bad := range []string{"short", strings.Repeat("x", 129), strings.Repeat("x", 32) + "/", strings.Repeat("x", 32) + "\n"} {
		if _, err := p.ApplicationAuthorizationURL(bad, redirect); err == nil {
			t.Fatal("invalid state accepted")
		}
	}
	for _, bad := range [][]AlipayAuthorizationAppType{nil, {"UNKNOWN"}, {AlipayAuthorizationWebApp, AlipayAuthorizationWebApp}} {
		if _, err := p.AssignedApplicationAuthorizationURLs(state, redirect, bad); err == nil {
			t.Fatal("invalid assigned app types accepted")
		}
	}
}

func TestApplicationAuthorizationCertificateSNAndExpiry(t *testing.T) {
	cfg, platform, cert, _ := alipayCertificateFixture(t)
	cfg.AppID = appAuthorizationIssuer
	sn, err := alipayCertSN(cert)
	if err != nil {
		t.Fatal(err)
	}
	now := cfg.Now()
	responseSN, calls := sn, 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		params := map[string]string{}
		for key, values := range r.Form {
			params[key] = values[0]
		}
		if params["method"] != "alipay.open.auth.token.app" || params["app_cert_sn"] == "" || params["alipay_root_cert_sn"] == "" || !verifyRSA([]byte(AlipayRequestCanonical(params)), params["sign"], &cfg.PrivateKey.PublicKey) {
			t.Error("missing signed certificate fields")
		}
		node := strings.TrimSuffix(appAuthorizationResponse, "}") + `,"expires_in":"3600"}`
		json.NewEncoder(w).Encode(map[string]any{"alipay_open_auth_token_app_response": json.RawMessage(node), "sign": independentSignature(t, node, platform), "alipay_cert_sn": responseSN})
	}))
	defer upstream.Close()
	cfg.Now = func() time.Time { return now }
	cfg.HTTPClient = &http.Client{Transport: oauthTLSRoundTripper{t, upstream, "openapi.alipay.com"}}
	p, err := NewAlipayApplicationAuthorization(cfg)
	if err != nil {
		t.Fatal(err)
	}
	in := ApplicationAuthorizationExchange{Code: "certificate-code", ExpectedAuthAppID: appAuthorizationTarget}
	if out, err := p.ExchangeApplicationAuthorization(context.Background(), in); err != nil || out.TokenExpiresInSeconds != 3600 {
		t.Fatal("valid certificate token rejected", err)
	}
	for _, badSN := range []string{"unknown", ""} {
		responseSN = badSN
		if _, err := p.ExchangeApplicationAuthorization(context.Background(), in); !errors.Is(err, ErrApplicationAuthorization) {
			t.Fatal("unknown or missing certificate accepted")
		}
	}
	now = now.Add(time.Hour)
	if _, err := p.ExchangeApplicationAuthorization(context.Background(), in); !errors.Is(err, ErrApplicationAuthorization) || calls != 3 {
		t.Fatal("expired certificate authorized or made HTTP request")
	}
	if _, err := p.ApplicationAuthorizationURL(strings.Repeat("a", 64), "https://pay.example.invalid/cb/"); !errors.Is(err, ErrApplicationAuthorization) {
		t.Fatal("expired certificate launch accepted")
	}
}

func TestApplicationAuthorizationDoesNotFollowRedirectOrRetry(t *testing.T) {
	key := testKey(t)
	calls := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Location", "https://openapi.alipay.com/untrusted")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()
	p, err := NewAlipayApplicationAuthorization(AlipayConfig{AppID: appAuthorizationIssuer, PrivateKey: key, PublicKey: &key.PublicKey, HTTPClient: &http.Client{Transport: oauthTLSRoundTripper{t, upstream, "openapi.alipay.com"}, CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ExchangeApplicationAuthorization(context.Background(), ApplicationAuthorizationExchange{Code: "synthetic-code", ExpectedAuthAppID: appAuthorizationTarget}); !errors.Is(err, ErrApplicationAuthorization) || calls != 1 {
		t.Fatal("redirect followed or request retried")
	}
	// The authorization-only capability cannot be mistaken for user OAuth or a
	// payment/transfer adapter by callers using existing capability interfaces.
	var capability any = p
	if _, ok := capability.(Provider); ok {
		t.Fatal("authorization exposes payment methods")
	}
	if _, ok := capability.(WebOAuthProvider); ok {
		t.Fatal("authorization exposes user OAuth")
	}
	if _, ok := capability.(MiniOAuthProvider); ok {
		t.Fatal("authorization exposes mini OAuth")
	}
}

// Changing the method, signature coverage, expected AppID, token JSON tag or
// refresh retention must break this test. All HTTP reaches local TLS only.
func TestApplicationAuthorizationExchangeBindsSignedAppWithoutFunds(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	calls := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/gateway.do" || r.URL.RawQuery != "" {
			t.Error("wrong fixed token endpoint")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		params := map[string]string{}
		for k, v := range r.Form {
			if len(v) != 1 {
				t.Error("duplicate request parameter")
			}
			params[k] = v[0]
		}
		if params["method"] != "alipay.open.auth.token.app" || params["app_id"] != appAuthorizationIssuer || params["biz_content"] != `{"code":"synthetic-code","grant_type":"authorization_code"}` || params["code"] != "" || params["grant_type"] != "" || params["app_auth_token"] != "" || !verifyRSA([]byte(AlipayRequestCanonical(params)), params["sign"], &merchant.PublicKey) {
			t.Error("wrong signed application authorization request")
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_open_auth_token_app_response": json.RawMessage(appAuthorizationResponse), "sign": independentSignature(t, appAuthorizationResponse, platform)})
	}))
	defer upstream.Close()
	p, err := NewAlipayApplicationAuthorization(AlipayConfig{AppID: appAuthorizationIssuer, PrivateKey: merchant, PublicKey: &platform.PublicKey, HTTPClient: &http.Client{Transport: oauthTLSRoundTripper{t, upstream, "openapi.alipay.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.ExchangeApplicationAuthorization(context.Background(), ApplicationAuthorizationExchange{Code: "synthetic-code", ExpectedAuthAppID: appAuthorizationTarget})
	if err != nil || out.Provider != "alipay" || out.IssuerAppID != appAuthorizationIssuer || out.AuthAppID != appAuthorizationTarget || out.UserID != "2088000000000000" || out.AppAuthToken != "synthetic-app-token" || out.TokenExpiresInSeconds != 0 {
		t.Fatal("wrong application authorization result", err)
	}
	raw, err := json.Marshal(out)
	if err != nil || strings.Contains(string(raw), "synthetic-app-token") || strings.Contains(string(raw), "discard-refresh") || strings.Contains(string(raw), "subject") {
		t.Fatal("credential leaked into public result JSON")
	}
	if calls != 1 {
		t.Fatal("exchange retried")
	}
}

func TestApplicationAuthorizationRejectsUntrustedResponseAndInput(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	cases := []struct {
		name, node, field, signature, envelope string
		status                                 int
	}{
		{name: "wrong_app", node: strings.Replace(appAuthorizationResponse, appAuthorizationTarget, appAuthorizationIssuer, 1)},
		{name: "missing_app", node: strings.Replace(appAuthorizationResponse, `"auth_app_id":"2021000000000002",`, "", 1)},
		{name: "bad_user", node: strings.Replace(appAuthorizationResponse, "2088000000000000", "not-a-user", 1)},
		{name: "empty_token", node: strings.Replace(appAuthorizationResponse, "synthetic-app-token", "", 1)},
		{name: "excluded_token", node: strings.Replace(appAuthorizationResponse, "synthetic-app-token", "@untrusted", 1)},
		{name: "long_token", node: strings.Replace(appAuthorizationResponse, "synthetic-app-token", strings.Repeat("x", 4097), 1)},
		{name: "duplicate_node_field", node: strings.Replace(appAuthorizationResponse, `"code":"10000"`, `"code":"40004","code":"10000"`, 1)},
		{name: "nested_duplicate", node: strings.TrimSuffix(appAuthorizationResponse, "}") + `,"extra":{"x":1,"x":2}}`},
		{name: "wrong_signature", signature: "invalid"},
		{name: "wrong_node", field: "alipay_system_oauth_token_response"},
		{name: "error_fallback", field: "error_response"},
		{name: "error_code", node: strings.Replace(appAuthorizationResponse, "10000", "40004", 1)},
		{name: "missing_code", node: strings.Replace(appAuthorizationResponse, `"code":"10000",`, "", 1)},
		{name: "zero_expiry", node: strings.TrimSuffix(appAuthorizationResponse, "}") + `,"expires_in":"0"}`},
		{name: "negative_expiry", node: strings.TrimSuffix(appAuthorizationResponse, "}") + `,"expires_in":-1}`},
		{name: "float_expiry", node: strings.TrimSuffix(appAuthorizationResponse, "}") + `,"expires_in":3.5}`},
		{name: "oversize", envelope: strings.Repeat(" ", 65537)},
		{name: "duplicate_envelope", envelope: `{"sign":"first","sign":"second"}`},
		{name: "trailing_json", envelope: `{} {}`},
		{name: "http_failure", status: 503},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				if tc.envelope != "" {
					w.Write([]byte(tc.envelope))
					return
				}
				node, field := tc.node, tc.field
				if node == "" {
					node = appAuthorizationResponse
				}
				if field == "" {
					field = "alipay_open_auth_token_app_response"
				}
				sig := tc.signature
				if sig == "" {
					sig = independentSignature(t, node, platform)
				}
				json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(node), "sign": sig})
			}))
			defer upstream.Close()
			p, err := NewAlipayApplicationAuthorization(AlipayConfig{AppID: appAuthorizationIssuer, PrivateKey: merchant, PublicKey: &platform.PublicKey, HTTPClient: &http.Client{Transport: oauthTLSRoundTripper{t, upstream, "openapi.alipay.com"}}})
			if err != nil {
				t.Fatal(err)
			}
			out, err := p.ExchangeApplicationAuthorization(context.Background(), ApplicationAuthorizationExchange{Code: "synthetic-code", ExpectedAuthAppID: appAuthorizationTarget})
			if !errors.Is(err, ErrApplicationAuthorization) || out.AppAuthToken != "" || strings.Contains(err.Error(), "synthetic") || calls != 1 {
				t.Fatal("untrusted response accepted, leaked, or retried")
			}
		})
	}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("invalid input made external request") }))
	defer upstream.Close()
	p, err := NewAlipayApplicationAuthorization(AlipayConfig{AppID: appAuthorizationIssuer, PrivateKey: merchant, PublicKey: &platform.PublicKey, HTTPClient: &http.Client{Transport: oauthTLSRoundTripper{t, upstream, "openapi.alipay.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []ApplicationAuthorizationExchange{{Code: "synthetic-code"}, {Code: "", ExpectedAuthAppID: appAuthorizationTarget}, {Code: "@excluded", ExpectedAuthAppID: appAuthorizationTarget}, {Code: strings.Repeat("x", 4097), ExpectedAuthAppID: appAuthorizationTarget}, {Code: "bad\tcode", ExpectedAuthAppID: appAuthorizationTarget}, {Code: "synthetic-code", ExpectedAuthAppID: "wrong-app"}} {
		if _, err := p.ExchangeApplicationAuthorization(context.Background(), in); !errors.Is(err, ErrApplicationAuthorization) {
			t.Fatal("invalid input accepted")
		}
	}
	for _, gateway := range []string{"http://openapi.alipay.com/gateway.do", "https://evil.example.invalid/gateway.do"} {
		if _, err := NewAlipayApplicationAuthorization(AlipayConfig{AppID: appAuthorizationIssuer, PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: gateway}); err == nil {
			t.Fatal("arbitrary gateway accepted")
		}
	}
}
