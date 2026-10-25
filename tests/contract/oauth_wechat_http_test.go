package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type oauthWechatTLSTransport struct {
	server *httptest.Server
	t      *testing.T
}

func (x oauthWechatTLSTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || (r.URL.Host != "api.weixin.qq.com" && r.URL.Host != "api.mch.weixin.qq.com") {
		x.t.Fatal("OAuth/payment request escaped official TLS host", r.URL.Host)
	}
	clone := r.Clone(r.Context())
	u := *clone.URL
	target, _ := url.Parse(x.server.URL)
	u.Host = target.Host
	clone.URL = &u
	return x.server.Client().Transport.RoundTrip(clone)
}

func TestWebOAuthWechatHTTPUsesAuthenticatedOpenIDWithoutCreditingPayment(t *testing.T) {
	s, _, _ := fixture(t)
	s.Sandbox = false
	exchanges, creates := 0, 0
	key := "0123456789abcdef0123456789abcdef"
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sns/oauth2/access_token":
			exchanges++
			if r.URL.Query().Get("appid") != "wxapp" || r.URL.Query().Get("secret") != "synthetic-secret" || r.URL.Query().Get("code") != "wx-web-code" {
				t.Error("incorrect bound OAuth exchange")
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "discard-wx-token", "expires_in": 7200, "scope": "snsapi_base", "openid": "authenticated-wx-openid"})
		case "/pay/unifiedorder":
			creates++
			raw := new(bytes.Buffer)
			raw.ReadFrom(r.Body)
			if !strings.Contains(raw.String(), "<openid>authenticated-wx-openid</openid>") {
				t.Error("OAuth identity was not used for JSAPI")
			}
			out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "wxapp", "mch_id": "mch", "prepay_id": "pending-jsapi", "trade_type": "JSAPI"}
			out["sign"], _ = adapter.SignWechatV2(out, key, "MD5")
			w.Write(providerXML(out))
		default:
			t.Error("unexpected Wechat OAuth/payment method", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer upstream.Close()
	client := &http.Client{Transport: oauthWechatTLSTransport{upstream, t}}
	wx, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "wxapp", MerchantID: "mch", APIKey: key, Methods: []string{"jsapi"}, HTTPClient: client})
	if e != nil {
		t.Fatal(e)
	}
	oauth, e := adapter.NewWechatOAuth(adapter.WechatOAuthConfig{AppID: "wxapp", AppSecret: "synthetic-secret", HTTPClient: client})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: wx}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.OAuthProviders = map[int64]adapter.WebOAuthProvider{7: oauth}
	s.CheckoutKey = bytes.Repeat([]byte{8}, 32)
	s.MerchantChannels = map[int64][]int64{1: {7}}
	h := s.Handler()
	_, out := call(t, h, "POST", "/mapi.php", signed(compat.Params{"pid": "1", "out_trade_no": "wx-oauth", "money": "1.25", "name": "item", "type": "wxpay", "method": "jsapi", "channel_id": "7", "oauth": "1", "clientip": "192.0.2.5"}, "contract-secret"))
	if out["code"] != float64(1) {
		t.Fatal(out)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", out["payurl"].(string), nil))
	if w.Code != 302 {
		t.Fatal(w.Code, w.Body.String())
	}
	authorization, _ := url.Parse(w.Header().Get("Location"))
	if authorization.Host != "open.weixin.qq.com" || authorization.Query().Get("scope") != "snsapi_base" || authorization.Fragment != "wechat_redirect" {
		t.Fatal(authorization)
	}
	callback, _ := url.Parse(authorization.Query().Get("redirect_uri"))
	callback.RawQuery = url.Values{"state": {authorization.Query().Get("state")}, "code": {"wx-web-code"}}.Encode()
	request := httptest.NewRequest("GET", callback.String(), nil)
	request.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, request)
	if w.Code != 302 || exchanges != 1 || creates != 1 {
		t.Fatal(w.Code, w.Body.String(), exchanges, creates)
	}
	balance, _ := s.Store.Balance(context.Background(), 1)
	if balance.Available != 0 {
		t.Fatal("OAuth confirmation credited a payment", balance)
	}
}
