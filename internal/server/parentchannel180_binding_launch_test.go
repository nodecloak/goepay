package server

import (
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

type binding180OAuthTransport struct{ up *httptest.Server }

func (t binding180OAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.weixin.qq.com" {
		return nil, adapter.ErrInvalid
	}
	target, err := url.Parse(t.up.URL)
	if err != nil {
		return nil, err
	}
	copy := r.Clone(r.Context())
	copy.URL.Scheme = target.Scheme
	copy.URL.Host = target.Host
	return t.up.Client().Transport.RoundTrip(copy)
}

func TestParentChannel180WeixinMiniAppHandlerLaunchAndOriginalCompletionAfterRotation(t *testing.T) {
	for _, family := range []string{"v2", "v3"} {
		t.Run(family, func(t *testing.T) {
			setup, local, web, mini, state := binding180Ready(t)
			rotationConfig := map[string]any{"appkey": map[string]string{"operation": "replace", "value": setup.keyB}}
			rotationCertificates := map[string]any{"sslcert_path": map[string]string{"operation": "replace", "value": setup.certB}, "sslkey_path": map[string]string{"operation": "replace", "value": setup.pemB}}
			if family == "v3" {
				state, rotationConfig, rotationCertificates = binding180PrepareV3Launch(t, setup, local, state)
			}
			f := setup.f
			var exchanges, bad atomic.Int64
			oauth := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				exchanges.Add(1)
				q := r.URL.Query()
				if r.URL.Path != "/sns/jscode2session" || q.Get("appid") != "synthetic-mini-A" || q.Get("secret") != "synthetic-mini-secret-A" || q.Get("js_code") != "synthetic-one-use-code-A" {
					bad.Add(1)
					w.WriteHeader(403)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"appid": "synthetic-mini-A", "openid": "synthetic-launch-openid-A", "session_key": "synthetic-private-session-key"})
			}))
			defer oauth.Close()
			local.miniClient = &http.Client{Transport: binding180OAuthTransport{oauth}}
			p := compat.Params{"pid": "1", "out_trade_no": "binding180-app-launch", "type": "wxpay", "name": "synthetic app launch", "money": "100.00", "channel_id": setup.id, "method": "app", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
			p["sign"] = compat.SignMD5(p, "test-secret")
			form := url.Values{}
			for k, v := range p {
				form.Set(k, v)
			}
			req := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(response, req)
			var out map[string]any
			if json.Unmarshal(response.Body.Bytes(), &out) != nil || out["code"] != float64(1) {
				t.Fatal("app did not resolve original bound mini launch", response.Code, response.Body.String())
			}
			checkout, ok := out["payurl"].(string)
			if !ok || checkout == "" || setup.creates.Load() != 0 || exchanges.Load() != 0 {
				t.Fatal("launch sent financial or identity request")
			}
			order, err := f.s.Store.Order(t.Context(), 1, p["out_trade_no"])
			if err != nil {
				t.Fatal(err)
			}
			mini.AppID = "synthetic-mini-B"
			mini.Revision = 2
			mini.Config = map[string]string{"appsecret": "synthetic-mini-secret-B"}
			mini.Path = "pages/new-pay"
			next, err := channelconfig.NewParentChannelFactory(local.Bootstrap(), channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}, IdentityProfiles: []channelconfig.IdentityProfileSpec{web, mini}, OAuthEnabled: true})
			if err != nil {
				t.Fatal(err)
			}
			local.ParentChannelFactory = next
			patch, _ := json.Marshal(map[string]any{"expected_fingerprint": state["fingerprint"], "account": map[string]any{"appwxa": "82", "expected_bindings_fingerprint": next.ParentWeixinCatalog().Fingerprint, "config": rotationConfig, "certificates": rotationCertificates}})
			changed := parent180HTTP(f, "PATCH", "/"+setup.id, string(patch))
			if changed.Code != 200 {
				t.Fatal(changed.Code, changed.Body.String())
			}
			req = httptest.NewRequest("GET", checkout, nil)
			response = httptest.NewRecorder()
			f.s.Handler().ServeHTTP(response, req)
			if response.Code != 200 {
				t.Fatal("original launch unavailable after profile rotation", response.Code, response.Body.String())
			}
			match := regexp.MustCompile(`<pre>(.*?)</pre>`).FindStringSubmatch(response.Body.String())
			if len(match) != 2 {
				t.Fatal("actual checkout omitted mini launch material")
			}
			var material map[string]string
			if json.Unmarshal([]byte(html.UnescapeString(match[1])), &material) != nil || material["appId"] != "synthetic-mini-A" {
				t.Fatal("original launch changed app", material)
			}
			page, err := url.Parse(material["path"])
			if err != nil || page.Path != "pages/pay" {
				t.Fatal("original launch changed page", material, err)
			}
			complete := page.Query().Get("url")
			if !strings.Contains(complete, order.TradeNo) {
				t.Fatal("completion lost original order")
			}
			send := func() int {
				request := httptest.NewRequest("POST", complete, strings.NewReader(`{"code":"synthetic-one-use-code-A"}`))
				request.Header.Set("Content-Type", "application/json")
				result := httptest.NewRecorder()
				f.s.Handler().ServeHTTP(result, request)
				if result.Code != 200 && setup.creates.Load() == 0 {
					t.Log("completion rejection", result.Code, result.Body.String())
				}
				return result.Code
			}
			if status := send(); status != 200 {
				t.Fatal("original app completion failed", status, "identity calls", exchanges.Load(), "bad identity", bad.Load(), "creates", setup.creates.Load())
			}
			if status := send(); status != 400 || exchanges.Load() != 1 || setup.creates.Load() != 1 || bad.Load() != 0 || setup.bad.Load() != 0 {
				t.Fatal("one-use completion repeated I/O", status, exchanges.Load(), setup.creates.Load(), bad.Load(), setup.bad.Load())
			}
		})
	}
}
