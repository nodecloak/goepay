package contract_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type oauthContractTLSTransport struct {
	server *httptest.Server
	t      *testing.T
}

func (x oauthContractTLSTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "openapi.alipay.com" {
		x.t.Fatal("OAuth/payment request escaped official TLS host", r.URL.Host)
	}
	clone := r.Clone(r.Context())
	u := *clone.URL
	target, _ := url.Parse(x.server.URL)
	u.Host = target.Host
	clone.URL = &u
	return x.server.Client().Transport.RoundTrip(clone)
}

func TestWebOAuthHTTPStateBrowserAndTrustedPayerSubmission(t *testing.T) {
	runWebOAuthHTTPStateBrowserAndTrustedPayerSubmissionWithOriginalGroupFixture(t, false)
}
func TestGroupRoutingUXHTTPWebOAuthHTTPStateBrowserAndTrustedPayerSubmission(t *testing.T) {
	runWebOAuthHTTPStateBrowserAndTrustedPayerSubmissionWithOriginalGroupFixture(t, true)
}
func TestChannel168WebOAuthHTTPStateBrowserAndTrustedPayerSubmission(t *testing.T) {
	runWebOAuthHTTPStateBrowserAndTrustedPayerSubmissionWithOriginalGroupFixture(t, true, installChannel168Fees)
}
func runWebOAuthHTTPStateBrowserAndTrustedPayerSubmissionWithOriginalGroupFixture(t *testing.T, groupRouting bool, channelFees ...channel168FixturePolicy) {
	s, pool, key := fixture(t)
	if groupRouting {
		installGroupRoutingEntrypointFixture152(t, s, map[string]string{"alipay": "7"})
	}
	s.Sandbox = false
	ctx := context.Background()
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	exchanges, creates := 0, 0
	var onExchange func()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		method := r.Form.Get("method")
		var node map[string]any
		field := ""
		switch method {
		case "alipay.system.oauth.token":
			exchanges++
			if onExchange != nil {
				onExchange()
			}
			field = "alipay_system_oauth_token_response"
			node = map[string]any{"user_id": "2088000000000000", "access_token": "must-not-leak-token", "expires_in": "3600"}
		case "alipay.trade.create":
			creates++
			var biz map[string]any
			json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
			if biz["buyer_id"] != "2088000000000000" {
				t.Error("untrusted payer submitted", biz)
			}
			field = "alipay_trade_create_response"
			node = map[string]any{"code": "10000", "out_trade_no": biz["out_trade_no"], "trade_no": "pending-client-payment"}
		default:
			t.Error("OAuth incorrectly called payment/other method", method)
			w.WriteHeader(500)
			return
		}
		raw, _ := json.Marshal(node)
		digest := sha256.Sum256(raw)
		signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signature)})
	}))
	defer upstream.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &platform.PublicKey, Methods: []string{"jsapi"}, HTTPClient: &http.Client{Transport: oauthContractTLSTransport{upstream, t}}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.OAuthProviders = map[int64]adapter.WebOAuthProvider{7: ali}
	s.CheckoutKey = bytes.Repeat([]byte{7}, 32)
	s.MerchantChannels = map[int64][]int64{1: {7}}
	if len(channelFees) > 0 && channelFees[0] != nil {
		applyChannel168Fixture(t, s, channelFees[0])
	}
	h := s.Handler()
	lastCheckout := ""
	start := func(no string) (string, string, *http.Cookie) {
		t.Helper()
		p := compat.Params{"pid": "1", "out_trade_no": no, "money": "1.25", "name": "deposit", "type": "alipay", "method": "jsapi", "channel_id": "7", "oauth": "1", "clientip": "192.0.2.5"}
		_, out := call(t, h, "POST", "/mapi.php", signed(p, "contract-secret"))
		if out["code"] != float64(1) {
			t.Fatal("explicit OAuth creation unavailable", out)
		}
		link := out["payurl"].(string)
		lastCheckout = link
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", link, nil))
		if w.Code != 302 {
			t.Fatal("OAuth checkout did not authorize", w.Code, w.Body.String())
		}
		u, e := url.Parse(w.Header().Get("Location"))
		if e != nil || u.Host != "openauth.alipay.com" || u.Query().Get("scope") != "auth_base" {
			t.Fatal(u, e)
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != "__Host-goepay_oauth" || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
			t.Fatal("unsafe OAuth binding cookie", cookies)
		}
		return u.Query().Get("redirect_uri"), u.Query().Get("state"), cookies[0]
	}
	callback, state, cookie := start("oauth-browser")
	if exchanges != 0 || creates != 0 {
		t.Fatal("authorization URL exchanged before callback")
	}
	repeat := httptest.NewRecorder()
	repeatReq := httptest.NewRequest("GET", lastCheckout, nil)
	repeatReq.AddCookie(cookie)
	h.ServeHTTP(repeat, repeatReq)
	if repeat.Code != 302 || len(repeat.Result().Cookies()) != 1 || repeat.Result().Cookies()[0].Value != cookie.Value {
		t.Fatal("second browser tab replaced OAuth cookie", repeat.Code)
	}
	secondAuth, _ := url.Parse(repeat.Header().Get("Location"))
	secondState := secondAuth.Query().Get("state")
	checkoutURL, _ := url.Parse(lastCheckout)
	trade := strings.TrimPrefix(checkoutURL.Path, "/checkout/")
	qrURL := *checkoutURL
	qrURL.Path += "/qr.png"
	for _, path := range []string{qrURL.String(), s.ProviderReturnURL(trade)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code == 302 {
			t.Fatal("non-checkout path issued OAuth state", path)
		}
	}
	invoke := func(callback, state, code string, c *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		u, _ := url.Parse(callback)
		q := url.Values{"state": {state}, "auth_code": {code}, "app_id": {"app"}, "scope": {"auth_base"}}
		u.RawQuery = q.Encode()
		r := httptest.NewRequest("GET", u.String(), nil)
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	wrong := *cookie
	wrong.Value = strings.Repeat("0", 64)
	if w := invoke(callback, state, "oauth-code", &wrong); w.Code != 400 || exchanges != 0 {
		t.Fatal("foreign browser reached token exchange", w.Code, exchanges)
	}
	altered := strings.Replace(callback, "/7/", "/8/", 1)
	if w := invoke(altered, state, "oauth-code", cookie); w.Code == 302 || exchanges != 0 {
		t.Fatal("channel binding ignored", w.Code, exchanges)
	}
	w := invoke(callback, state, "oauth-code", cookie)
	if w.Code != 302 || exchanges != 1 || creates != 1 {
		t.Fatal("trusted identity not submitted once", w.Code, w.Body.String(), exchanges, creates)
	}
	if strings.Contains(w.Header().Get("Location"), "oauth-code") || strings.Contains(w.Body.String(), "must-not-leak-token") {
		t.Fatal("OAuth secret echoed")
	}
	if w = invoke(callback, state, "oauth-code", cookie); w.Code != 400 || exchanges != 1 || creates != 1 {
		t.Fatal("state/code replay reached provider", w.Code, exchanges, creates)
	}
	if w = invoke(callback, secondState, "second-tab-code", cookie); w.Code != 400 || exchanges != 1 || creates != 1 {
		t.Fatal("existing attempt replaced payer through another tab", w.Code, exchanges, creates)
	}
	for i, extra := range []compat.Params{{"oauth": "true"}, {"oauth": "1", "is_applet": "1"}, {"oauth": "1", "sub_appid": "foreign-app"}, {"oauth": "1", "sub_openid": "2088000000000000"}, {"oauth": "0"}} {
		p := compat.Params{"pid": "1", "out_trade_no": fmt.Sprintf("bad-oauth-%d", i), "money": "1.25", "name": "item", "type": "alipay", "method": "jsapi", "channel_id": "7"}
		for k, v := range extra {
			p[k] = v
		}
		_, out := call(t, h, "POST", "/mapi.php", signed(p, "contract-secret"))
		if out["code"] != float64(-1) {
			t.Fatal("ambiguous OAuth route accepted", extra, out)
		}
	}
	b, _ := s.Store.Balance(ctx, 1)
	if b.Available != 0 {
		t.Fatal("OAuth was treated as payment", b)
	}
	blockedCallback, blockedState, blockedCookie := start("oauth-blocked")
	if _, e = pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('blacklist','{"kind":"buyer","value":"2088000000000000","reason":"test","enabled":true}')`); e != nil {
		t.Fatal(e)
	}
	w = invoke(blockedCallback, blockedState, "blocked-code", blockedCookie)
	if w.Code != 403 || exchanges != 2 || creates != 1 {
		t.Fatal("OAuth bypassed buyer blacklist", w.Code, exchanges, creates)
	}
	for _, kind := range []string{"stop", "whitelist", "appid", "merchant"} {
		cb, st, c := start("revoked-" + kind)
		switch kind {
		case "stop":
			s.ProviderFlow.AllowLive = false
		case "whitelist":
			s.MerchantChannels[1] = nil
		case "appid":
			other, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "different-app", PrivateKey: key, PublicKey: &platform.PublicKey, Methods: []string{"jsapi"}})
			if e != nil {
				t.Fatal(e)
			}
			s.OAuthProviders[7] = other
		case "merchant":
			if _, e = pool.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=1"); e != nil {
				t.Fatal(e)
			}
		}
		w = invoke(cb, st, "revoked-code-"+kind, c)
		if w.Code == 302 || exchanges != 2 || creates != 1 {
			t.Fatal("revoked OAuth reached exchange", kind, w.Code, exchanges, creates)
		}
		s.ProviderFlow.AllowLive = true
		s.MerchantChannels[1] = []int64{7}
		s.OAuthProviders[7] = ali
		if _, e = pool.Exec(ctx, "UPDATE merchants SET enabled=true WHERE id=1"); e != nil {
			t.Fatal(e)
		}
		if w = invoke(cb, st, "revoked-code-"+kind, c); w.Code != 400 || exchanges != 2 {
			t.Fatal("restored config revived consumed authorization", kind, w.Code, exchanges)
		}
	}
	cb, st, c := start("stopped-during-exchange")
	onExchange = func() { s.ProviderFlow.AllowLive = false }
	w = invoke(cb, st, "mid-exchange-code", c)
	onExchange = nil
	s.ProviderFlow.AllowLive = true
	if w.Code != 503 || exchanges != 3 || creates != 1 {
		t.Fatal("stop during exchange bypassed payment gate", w.Code, exchanges, creates)
	}
	if _, e = pool.Exec(ctx, "DELETE FROM backoffice_records WHERE resource='blacklist'"); e != nil {
		t.Fatal(e)
	}
	for _, ip := range []string{"192.0.2.5", "192.0.2.1"} {
		cb, st, c := start("blocked-ip-" + strings.ReplaceAll(ip, ".", "-"))
		body, _ := json.Marshal(map[string]any{"kind": "ip", "value": ip, "reason": "test", "enabled": true})
		if _, e = pool.Exec(ctx, "INSERT INTO backoffice_records(resource,body) VALUES('blacklist',$1)", body); e != nil {
			t.Fatal(e)
		}
		before := exchanges
		w = invoke(cb, st, "ip-code-"+ip, c)
		if w.Code != 403 || exchanges != before+1 || creates != 1 {
			t.Fatal("OAuth bypassed original or callback IP restriction", ip, w.Code, exchanges, creates)
		}
		if _, e = pool.Exec(ctx, "DELETE FROM backoffice_records WHERE resource='blacklist'"); e != nil {
			t.Fatal(e)
		}
	}
	var stored string
	if e = pool.QueryRow(ctx, "SELECT (SELECT COALESCE(jsonb_agg(to_jsonb(c)),'[]'::jsonb)::text FROM oauth_code_claims c)||(SELECT COALESCE(jsonb_agg(to_jsonb(x)),'[]'::jsonb)::text FROM oauth_states x)||(SELECT COALESCE(jsonb_agg(to_jsonb(i)),'[]'::jsonb)::text FROM oauth_payment_intents i)").Scan(&stored); e != nil || strings.Contains(stored, "must-not-leak-token") || strings.Contains(stored, "oauth-code") || strings.Contains(stored, cookie.Value) || strings.Contains(stored, state) {
		t.Fatal("plaintext OAuth artifacts persisted", e)
	}
}
