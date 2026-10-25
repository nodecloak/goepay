package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type oauthTLSRoundTripper struct {
	t      *testing.T
	server *httptest.Server
	host   string
}

func (o oauthTLSRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	o.t.Helper()
	if r.URL.Scheme != "https" || r.URL.Host != o.host {
		o.t.Fatalf("OAuth escaped fixed TLS endpoint: %s", r.URL.Host)
	}
	clone := r.Clone(r.Context())
	target, _ := url.Parse(o.server.URL)
	address := *clone.URL
	address.Scheme = "https"
	address.Host = target.Host
	clone.URL = &address
	return o.server.Client().Transport.RoundTrip(clone)
}
func TestWebOAuthWechatFixedTLSExchangeAndNoSecretErrors(t *testing.T) {
	bad := false
	calls := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/sns/oauth2/access_token" || r.URL.Query().Get("appid") != "wxapp" || r.URL.Query().Get("secret") != "test-secret" || r.URL.Query().Get("code") != "test-code" {
			t.Error("wrong OAuth request")
		}
		if bad {
			w.WriteHeader(503)
			w.Write([]byte("test-secret test-code access-token"))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "access-token", "expires_in": 7200, "openid": "trusted-openid", "scope": "snsapi_base"})
	}))
	defer upstream.Close()
	p, e := NewWechatOAuth(WechatOAuthConfig{AppID: "wxapp", AppSecret: "test-secret", HTTPClient: &http.Client{Transport: oauthTLSRoundTripper{t, upstream, "api.weixin.qq.com"}}})
	if e != nil {
		t.Fatal(e)
	}
	redirect, e := p.OAuthAuthorizationURL(strings.Repeat("a", 64), "https://pay.example.invalid/oauth/callback/wxpay/7/trade/")
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(redirect)
	if u.Host != "open.weixin.qq.com" || u.Query().Get("scope") != "snsapi_base" || u.Query().Get("appid") != "wxapp" {
		t.Fatal(redirect)
	}
	identity, e := p.ExchangeOAuth(context.Background(), "test-code")
	if e != nil || identity.AppID != "wxapp" || identity.Provider != "wxpay" || identity.SubjectID != "trusted-openid" {
		t.Fatal(identity, e)
	}
	bad = true
	_, e = p.ExchangeOAuth(context.Background(), "test-code")
	if e == nil || strings.Contains(e.Error(), "test-secret") || strings.Contains(e.Error(), "test-code") || strings.Contains(e.Error(), "access-token") {
		t.Fatal("secret leaked through exchange error", e)
	}
	if _, e = p.OAuthAuthorizationURL(strings.Repeat("a", 64), "http://pay.example.invalid/oauth/callback"); e == nil {
		t.Fatal("non TLS callback accepted")
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
func TestWebOAuthAlipayUsesTopLevelCodeAndSignedExactTokenNode(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	bad := false
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		params := map[string]string{}
		for k, v := range r.Form {
			params[k] = v[0]
		}
		if r.URL.Path != "/gateway.do" || params["method"] != "alipay.system.oauth.token" || params["grant_type"] != "authorization_code" || params["code"] != "test-code" || params["biz_content"] != "" || !verifyRSA([]byte(AlipayRequestCanonical(params)), params["sign"], &merchant.PublicKey) {
			t.Error("wrong signed OAuth token request")
		}
		raw := `{"user_id":"2088000000000000","access_token":"access-token","expires_in":"3600"}`
		sig := independentSignature(t, raw, platform)
		if bad {
			sig = "invalid"
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_system_oauth_token_response": json.RawMessage(raw), "sign": sig})
	}))
	defer upstream.Close()
	p, e := NewAlipay(AlipayConfig{AppID: "aliapp", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: "http://must-not-use.example.invalid", HTTPClient: &http.Client{Transport: oauthTLSRoundTripper{t, upstream, "openapi.alipay.com"}}})
	if e != nil {
		t.Fatal(e)
	}
	identity, e := p.ExchangeOAuth(context.Background(), "test-code")
	if e != nil || identity.SubjectID != "2088000000000000" || identity.AppID != "aliapp" {
		t.Fatal(identity, e)
	}
	bad = true
	if _, e = p.ExchangeOAuth(context.Background(), "test-code"); e == nil {
		t.Fatal("unsigned token identity accepted")
	}
	if _, e = p.ExchangeOAuth(context.Background(), "@unsigned-code"); e == nil {
		t.Fatal("AOP excluded code accepted")
	}
}

func TestWebOAuthCertificateTokenResponsePinsSNAndLifetime(t *testing.T) {
	config, platform, platformCert, _ := alipayCertificateFixture(t)
	sn, e := alipayCertSN(platformCert)
	if e != nil {
		t.Fatal(e)
	}
	wrongSN := false
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("app_cert_sn") == "" || r.Form.Get("alipay_root_cert_sn") == "" || r.Form.Get("biz_content") != "" {
			t.Error("certificate OAuth request missing trusted SN")
		}
		node := `{"user_id":"2088000000000000","access_token":"certificate-token","expires_in":"3600"}`
		responseSN := sn
		if wrongSN {
			responseSN = "unknown-certificate"
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_system_oauth_token_response": json.RawMessage(node), "sign": independentSignature(t, node, platform), "alipay_cert_sn": responseSN})
	}))
	defer upstream.Close()
	config.HTTPClient = &http.Client{Transport: oauthTLSRoundTripper{t, upstream, "openapi.alipay.com"}}
	p, e := NewAlipay(config)
	if e != nil {
		t.Fatal(e)
	}
	if identity, e := p.ExchangeOAuth(context.Background(), "certificate-code"); e != nil || identity.SubjectID != "2088000000000000" {
		t.Fatal(identity, e)
	}
	wrongSN = true
	if _, e = p.ExchangeOAuth(context.Background(), "certificate-code-2"); e == nil {
		t.Fatal("untrusted OAuth response certificate accepted")
	}
}
