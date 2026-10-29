package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMiniOAuthWechatHTTPLaunchToTrustedJSAPIPayer(t *testing.T) {
	runMiniOAuthWechatHTTPLaunchToTrustedJSAPIPayerWithOriginalGroupFixture(t, false)
}
func TestGroupRoutingUXHTTPMiniOAuthWechatHTTPLaunchToTrustedJSAPIPayer(t *testing.T) {
	runMiniOAuthWechatHTTPLaunchToTrustedJSAPIPayerWithOriginalGroupFixture(t, true)
}
func TestChannel168MiniOAuthWechatHTTPLaunchToTrustedJSAPIPayer(t *testing.T) {
	runMiniOAuthWechatHTTPLaunchToTrustedJSAPIPayerWithOriginalGroupFixture(t, true, installChannel168Fees)
}
func runMiniOAuthWechatHTTPLaunchToTrustedJSAPIPayerWithOriginalGroupFixture(t *testing.T, groupRouting bool, channelFees ...channel168FixturePolicy) {
	s, pool, _ := fixture(t)
	if groupRouting {
		installGroupRoutingEntrypointFixture152(t, s, map[string]string{"wxpay": "7"})
	}
	s.Sandbox = false
	ctx := context.Background()
	exchanges, creates := 0, 0
	settlementTrade := ""
	key := "0123456789abcdef0123456789abcdef"
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sns/jscode2session":
			exchanges++
			if r.URL.Query().Get("appid") != "wxapp" || r.URL.Query().Get("secret") != "synthetic-mini-secret" || r.URL.Query().Get("js_code") != "wx-mini-code" {
				t.Error("wrong mini credentials/code")
			}
			json.NewEncoder(w).Encode(map[string]any{"openid": "authenticated-mini-openid", "session_key": "must-discard-session-key"})
		case "/pay/unifiedorder":
			creates++
			raw := new(bytes.Buffer)
			raw.ReadFrom(r.Body)
			if !strings.Contains(raw.String(), "<openid>authenticated-mini-openid</openid>") || !strings.Contains(raw.String(), "<trade_type>JSAPI</trade_type>") {
				t.Error("mini used unverified payer or wrong payment product")
			}
			out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "wxapp", "mch_id": "mch", "prepay_id": "mini-pending-jsapi", "trade_type": "JSAPI"}
			out["sign"], _ = adapter.SignWechatV2(out, key, "MD5")
			w.Write(providerXML(out))
		case "/pay/orderquery":
			out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "wxapp", "mch_id": "mch", "out_trade_no": settlementTrade, "transaction_id": "mini-final-payment", "trade_state": "SUCCESS", "total_fee": "125", "fee_type": "CNY", "openid": "authenticated-mini-openid"}
			out["sign"], _ = adapter.SignWechatV2(out, key, "MD5")
			w.Write(providerXML(out))
		default:
			t.Error("unexpected mini endpoint", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer up.Close()
	client := &http.Client{Transport: oauthWechatTLSTransport{up, t}}
	wx, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "wxapp", MerchantID: "mch", APIKey: key, Methods: []string{"jsapi"}, MiniProgram: &adapter.MiniProgramConfig{Path: "pages/pay/pay"}, HTTPClient: client})
	if e != nil {
		t.Fatal(e)
	}
	mini, e := adapter.NewWechatMiniOAuth(adapter.WechatOAuthConfig{AppID: "wxapp", AppSecret: "synthetic-mini-secret", HTTPClient: client})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: wx}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.MiniOAuthProviders = map[int64]adapter.MiniOAuthProvider{7: mini}
	s.CheckoutKey = bytes.Repeat([]byte{8}, 32)
	s.MerchantChannels = map[int64][]int64{1: {7}}
	if len(channelFees) > 0 && channelFees[0] != nil {
		applyChannel168Fixture(t, s, channelFees[0])
	}
	h := s.Handler()
	_, out := call(t, h, "POST", "/mapi.php", signed(compat.Params{"pid": "1", "out_trade_no": "wx-mini", "money": "1.25", "name": "item", "type": "wxpay", "method": "applet", "channel_id": "7", "is_applet": "1", "clientip": "192.0.2.5", "notify_url": "https://merchant.example.invalid/notify"}, "contract-secret"))
	if out["code"] != float64(1) {
		t.Fatal(out)
	}
	launch := httptest.NewRecorder()
	h.ServeHTTP(launch, httptest.NewRequest("GET", out["payurl"].(string), nil))
	if launch.Code != 200 || exchanges != 0 || creates != 0 {
		t.Fatal("launch performed identity/payment I/O", launch.Code)
	}
	b, e := s.Store.MiniPaymentIntent(ctx, out["trade_no"].(string))
	if e != nil {
		t.Fatal(e)
	}
	markup := launch.Body.String()
	a, z := strings.Index(markup, "<pre>"), strings.Index(markup, "</pre>")
	if a < 0 || z <= a {
		t.Fatal("missing SDK launch material")
	}
	var material map[string]string
	if e = json.Unmarshal([]byte(html.UnescapeString(markup[a+5:z])), &material); e != nil || material["appId"] != "wxapp" {
		t.Fatal(material, e)
	}
	page, e := url.Parse(material["path"])
	if e != nil || page.Path != "pages/pay/pay" {
		t.Fatal(page, e)
	}
	completion, e := url.Parse(page.Query().Get("url"))
	if e != nil || completion.Host != "pay.example.invalid" || completion.Path != "/oauth/mini/complete/wxpay/7/"+b.TradeNo+"/" {
		t.Fatal(completion, e)
	}
	state := completion.Query().Get("state")
	path := completion.String()
	req := httptest.NewRequest("POST", path, strings.NewReader(`{"code":"wx-mini-code","state":"`+state+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.4:42000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || exchanges != 1 || creates != 1 {
		t.Fatal(w.Code, w.Body.String(), exchanges, creates)
	}
	if strings.Contains(w.Body.String(), "must-discard-session-key") || strings.Contains(w.Body.String(), "authenticated-mini-openid") || strings.Contains(w.Body.String(), "wx-mini-code") {
		t.Fatal("SDK response leaked identity exchange")
	}
	bal, e := s.Store.Balance(ctx, 1)
	if e != nil || bal.Available != 0 {
		t.Fatal("mini confirmation credited merchant", bal, e)
	}
	o, e := s.Store.OrderByTradeNo(ctx, b.TradeNo)
	if e != nil || o.Status == "paid" {
		t.Fatal(o, e)
	}
	settlementTrade = b.TradeNo
	for i := 0; i < 2; i++ {
		if _, e = s.ProviderFlow.Query(ctx, 1, b.TradeNo); e != nil {
			t.Fatal("trusted mini JSAPI query failed", e)
		}
	}
	notice := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "wxapp", "mch_id": "mch", "out_trade_no": b.TradeNo, "transaction_id": "mini-final-payment", "total_fee": "125", "fee_type": "CNY", "openid": "authenticated-mini-openid"}
	notice["sign"], _ = adapter.SignWechatV2(notice, key, "MD5")
	for i := 0; i < 2; i++ {
		if e = s.ProviderFlow.HandleNotification(ctx, 7, b.TradeNo, nil, providerXML(notice)); e != nil {
			t.Fatal("late mini JSAPI notification failed", e)
		}
	}
	bal, e = s.Store.Balance(ctx, 1)
	wantCredit := int64(125)
	if len(channelFees) > 0 && channelFees[0] != nil {
		wantCredit = 124
	}
	if e != nil || bal.Available != wantCredit {
		t.Fatal("mini query/notification credited more than once", bal, e)
	}
	var receipts int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM verified_payment_receipts WHERE order_id=$1", o.ID).Scan(&receipts); e != nil || receipts != 1 {
		t.Fatal(receipts, e)
	}
}
