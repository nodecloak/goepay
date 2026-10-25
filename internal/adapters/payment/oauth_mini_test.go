package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiniOAuthWechatFixedTLSIdentityOnlyAndMalformedResponses(t *testing.T) {
	body := `{"openid":"trusted-mini-openid","session_key":"never-persist-session"}`
	calls := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		if r.URL.Path != "/sns/jscode2session" || q.Get("appid") != "wxmini" || q.Get("secret") != "synthetic-secret" || q.Get("js_code") != "synthetic-code" || q.Get("code") != "" || q.Get("grant_type") != "authorization_code" {
			t.Error("incorrect mini exchange")
		}
		w.Write([]byte(body))
	}))
	defer upstream.Close()
	p, e := NewWechatMiniOAuth(WechatOAuthConfig{AppID: "wxmini", AppSecret: "synthetic-secret", HTTPClient: &http.Client{Transport: oauthTLSRoundTripper{t, upstream, "api.weixin.qq.com"}}})
	if e != nil {
		t.Fatal(e)
	}
	identity, e := p.ExchangeMiniCode(context.Background(), "synthetic-code")
	if e != nil || identity.SubjectID != "trusted-mini-openid" || identity.Provider != "wxpay" || identity.AppID != "wxmini" {
		t.Fatal(identity, e)
	}
	for _, bad := range []string{`{"errcode":40029,"errmsg":"synthetic-code synthetic-secret never-persist-session"}`, `{"openid":"trusted-mini-openid"}`, `{"openid":"one","openid":"two","session_key":"key"}`, `{"openid":"one","session_key":"key","appid":"other"}`, `{"openid":"one","session_key":"key"} {}`} {
		body = bad
		identity, e = p.ExchangeMiniCode(context.Background(), "synthetic-code")
		if e == nil || identity.SubjectID != "" || strings.Contains(e.Error(), "synthetic") || strings.Contains(e.Error(), "never-persist") {
			t.Fatal("invalid mini response disclosed identity/secret", identity, e)
		}
	}
	before := calls
	if _, e = p.ExchangeMiniCode(context.Background(), "@unsigned"); e == nil || calls != before {
		t.Fatal("invalid code sent")
	}
}

func TestMiniOAuthAlipayCertificateExactTokenNodeAndPinnedSN(t *testing.T) {
	config, platform, platformCert, _ := alipayCertificateFixture(t)
	sn, e := alipayCertSN(platformCert)
	if e != nil {
		t.Fatal(e)
	}
	badSN, badSignature := false, false
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("method") != "alipay.system.oauth.token" || r.Form.Get("code") != "mini-cert-code" || r.Form.Get("app_cert_sn") == "" || r.Form.Get("alipay_root_cert_sn") == "" {
			t.Error("incorrect mini certificate request")
		}
		raw := `{"user_id":"2088000000000000","access_token":"discard-mini-cert-token","expires_in":"3600"}`
		signature := independentSignature(t, raw, platform)
		if badSignature {
			raw = `{"user_id":"2088111111111111","access_token":"discard-mini-cert-token","expires_in":"3600"}`
		}
		responseSN := sn
		if badSN {
			responseSN = "foreign-certificate"
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_system_oauth_token_response": json.RawMessage(raw), "sign": signature, "alipay_cert_sn": responseSN})
	}))
	defer upstream.Close()
	config.HTTPClient = &http.Client{Transport: oauthTLSRoundTripper{t, upstream, "openapi.alipay.com"}}
	p, e := NewAlipay(config)
	if e != nil {
		t.Fatal(e)
	}
	if identity, e := p.ExchangeMiniCode(context.Background(), "mini-cert-code"); e != nil || identity.SubjectID != "2088000000000000" {
		t.Fatal(identity, e)
	}
	badSN = true
	if _, e = p.ExchangeMiniCode(context.Background(), "mini-cert-code"); e == nil {
		t.Fatal("mini code accepted unknown certificate SN")
	}
	badSN = false
	badSignature = true
	if _, e = p.ExchangeMiniCode(context.Background(), "mini-cert-code"); e == nil {
		t.Fatal("mini code accepted altered signed identity")
	}
}
