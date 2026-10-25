package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMiniLaunchHTTPMaterialsHaveOneUseBoundStateButNoPaymentAttempt(t *testing.T) {
	s, pool, key := fixture(t)
	ctx := context.Background()
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("launch unexpectedly called payment or identity upstream")
		http.Error(w, "unexpected", 500)
	}))
	defer upstream.Close()
	wx, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "wxapp", MerchantID: "mch", APIKey: "0123456789abcdef0123456789abcdef", Methods: []string{"jsapi"}, MiniProgram: &adapter.MiniProgramConfig{Path: "pages/pay/pay"}, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "aliapp", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"mini"}, MiniProgram: &adapter.MiniProgramConfig{Path: "pages/pay/index"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	wxoauth, e := adapter.NewWechatMiniOAuth(adapter.WechatOAuthConfig{AppID: "wxapp", AppSecret: "synthetic-mini-secret", HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.Sandbox = false
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid", Providers: map[int64]adapter.Provider{7: wx, 8: ali}}
	s.MiniOAuthProviders = map[int64]adapter.MiniOAuthProvider{7: wxoauth, 8: ali}
	s.MerchantChannels = map[int64][]int64{1: {7, 8}, 2: {7, 8}}
	s.CheckoutKey = bytes.Repeat([]byte{9}, 32)
	invoke := func(legacy bool, p compat.Params) map[string]any {
		t.Helper()
		path := "/api/pay/create"
		p["pid"] = "2"
		p["sign_type"] = "RSA"
		p["timestamp"] = fmt.Sprint(time.Now().Unix())
		if legacy {
			path = "/mapi.php"
			p["pid"] = "1"
			delete(p, "timestamp")
			p["sign_type"] = "MD5"
			p["sign"] = compat.SignMD5(p, "contract-secret")
		} else {
			p["sign"], _ = compat.SignRSA(p, key)
		}
		v := url.Values{}
		for k, x := range p {
			v.Set(k, x)
		}
		_, out := call(t, s.Handler(), "POST", path, v)
		return out
	}
	for _, scenario := range []struct{ kind, channel, material string }{{"wxpay", "7", "wxapp"}, {"alipay", "8", "urlscheme"}} {
		p := compat.Params{"type": scenario.kind, "method": "applet", "is_applet": "1", "channel_id": scenario.channel, "out_trade_no": "mini-" + scenario.kind, "money": "1.25", "name": "mini launch", "clientip": "192.0.2.3"}
		out := invoke(false, p)
		if out["code"] != float64(0) || out["pay_type"] != scenario.material {
			t.Fatal("wrong mini launch response", out)
		}
		trade := out["trade_no"].(string)
		var page string
		if scenario.kind == "wxpay" {
			var material map[string]string
			if e = json.Unmarshal([]byte(out["pay_info"].(string)), &material); e != nil || material["appId"] != "wxapp" {
				t.Fatal(material, e)
			}
			page = material["path"]
		} else {
			scheme, err := url.Parse(out["pay_info"].(string))
			if err != nil || scheme.Scheme != "alipays" || scheme.Query().Get("appId") != "aliapp" {
				t.Fatal(out, err)
			}
			page = scheme.Query().Get("page")
		}
		parsed, e := url.Parse(page)
		if e != nil || parsed.Query().Get("money") != "1.25" {
			t.Fatal(page, e)
		}
		complete, e := url.Parse(parsed.Query().Get("url"))
		if e != nil || complete.Scheme != "https" || complete.Host != "pay.example.invalid" || !strings.HasSuffix(complete.Path, "/"+trade+"/") || len(complete.Query().Get("state")) != 64 {
			t.Fatal("unbound mini completion", complete, e)
		}
		b, e := s.Store.MiniPaymentIntent(ctx, trade)
		if e != nil || b.MerchantID != 2 || b.Purpose != "mini-payment" || b.Provider != scenario.kind {
			t.Fatal(b, e)
		}
		if e = s.Store.ConsumeMiniState(ctx, complete.Query().Get("state"), "synthetic-code-"+scenario.kind, b); e != nil {
			t.Fatal("generated state did not match stored context", e)
		}
		if e = s.Store.ConsumeMiniState(ctx, complete.Query().Get("state"), "new-code", b); e == nil {
			t.Fatal("mini state reused")
		}
	}
	// Legacy clients still receive only the protected payurl fallback.
	out := invoke(true, compat.Params{"type": "alipay", "method": "applet", "channel_id": "8", "out_trade_no": "mini-legacy", "money": "1.00", "name": "legacy mini"})
	if out["code"] != float64(1) || out["payurl"] == nil || out["pay_info"] != nil || out["pay_type"] != nil {
		t.Fatal("legacy material contract changed", out)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", out["payurl"].(string), nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "打开支付宝小程序") || !strings.Contains(w.Body.String(), "不代表付款成功") {
		t.Fatal("protected legacy mini checkout missing", w.Code, w.Body.String())
	}
	bad := httptest.NewRecorder()
	s.Handler().ServeHTTP(bad, httptest.NewRequest("GET", "/checkout/"+out["trade_no"].(string), nil))
	if bad.Code != 403 {
		t.Fatal("unprotected mini checkout", bad.Code)
	}
	legacyTrade := out["trade_no"].(string)
	qrLink, _ := url.Parse(out["payurl"].(string))
	qrLink.Path += "/qr.png"
	for path, status := range map[string]int{qrLink.String(): 404, s.ProviderReturnURL(legacyTrade): 202} {
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != status {
			t.Fatal("mini auxiliary route incorrectly minted material", path, w.Code)
		}
	}
	var issued int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM mini_oauth_states s JOIN orders o ON o.id=s.order_id WHERE o.trade_no=$1", legacyTrade).Scan(&issued); e != nil || issued != 1 {
		t.Fatal("non-launch routes minted state", issued, e)
	}
	for i := 0; i < 7; i++ {
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", out["payurl"].(string), nil))
		if w.Code != 200 {
			t.Fatal("bounded launch failed too early", i, w.Code)
		}
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", out["payurl"].(string), nil))
	if w.Code != 409 {
		t.Fatal("unbounded active launch states", w.Code)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM mini_oauth_states s JOIN orders o ON o.id=s.order_id WHERE o.trade_no=$1", legacyTrade).Scan(&issued); e != nil || issued != 8 {
		t.Fatal("unexpected active launch bound", issued, e)
	}
	var orders, attempts, receipts, credits int
	if e = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts),(SELECT count(*) FROM verified_payment_receipts),(SELECT count(*) FROM ledger_entries)").Scan(&orders, &attempts, &receipts, &credits); e != nil || orders != 3 || attempts != 0 || receipts != 0 || credits != 0 || calls.Load() != 0 {
		t.Fatal("launch produced a financial effect", orders, attempts, receipts, credits, calls.Load(), e)
	}
	for _, invalid := range []struct{ field, value string }{{"sub_openid", "caller-payer"}, {"sub_appid", "foreign-app"}, {"oauth", "1"}} {
		p := compat.Params{"type": "wxpay", "method": "applet", "channel_id": "7", "out_trade_no": "bad-mini-" + invalid.field, "money": "1.00", "name": "reject"}
		p[invalid.field] = invalid.value
		if out = invoke(false, p); out["code"] != float64(-1) {
			t.Fatal("caller identity override accepted", out)
		}
	}
	delete(s.MiniOAuthProviders, 7)
	if out = invoke(false, compat.Params{"type": "wxpay", "method": "applet", "channel_id": "7", "out_trade_no": "no-mini-exchange", "money": "1.00", "name": "reject"}); out["code"] != float64(-1) {
		t.Fatal("launch without configured SDK code exchange accepted", out)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM orders").Scan(&orders); e != nil || orders != 3 || calls.Load() != 0 {
		t.Fatal("invalid mini request created order", orders, e)
	}
	// A jump response must stay a protected browser URL even for mini-only products.
	for _, kind := range []string{"alipay"} {
		out = invoke(false, compat.Params{"type": kind, "method": "jump", "channel_id": "8", "out_trade_no": "jump-mini", "money": "1.00", "name": "mini jump"})
		if out["code"] != float64(0) || out["pay_type"] != "jump" || !strings.HasPrefix(fmt.Sprint(out["pay_info"]), "https://pay.example.invalid/checkout/") {
			t.Fatal("mini jump exposed raw launch material", out)
		}
		if e = pool.QueryRow(ctx, "SELECT count(*) FROM mini_oauth_states s JOIN orders o ON o.id=s.order_id WHERE o.trade_no=$1", out["trade_no"]).Scan(&issued); e != nil || issued != 0 {
			t.Fatal("jump minted state before checkout", issued, e)
		}
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", out["pay_info"].(string), nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "打开支付宝小程序") {
			t.Fatal("mini jump checkout unavailable", w.Code)
		}
	}
}
