package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"bytes"
	qrcode "github.com/skip2/go-qrcode"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
)

type wechatHTTP240Provider struct {
	*channel168Provider
	queries atomic.Int64
}

type wechatPorted240Provider struct{ *wechatHTTP240Provider }

func (*wechatPorted240Provider) PluginName() string { return "synthetic-ported240" }

func (*wechatHTTP240Provider) ID() string { return "wxpay" }
func (*wechatHTTP240Provider) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Methods: []string{"native", "h5", "jsapi", "app"}, Query: true}
}
func (p *wechatHTTP240Provider) Create(_ context.Context, r adapter.CreateRequest) (adapter.CreateResult, error) {
	p.sends.Add(1)
	switch r.Method {
	case "native":
		return adapter.CreateResult{MaterialType: "qrcode", Material: "weixin://wxpay/bizpayurl?pr=synthetic240"}, nil
	case "h5":
		return adapter.CreateResult{MaterialType: "jump", Material: "https://wx.tenpay.com/cgi-bin/mmpayweb-bin/checkmweb?prepay_id=synthetic240&redirect_url=https%3A%2F%2Fmerchant.example.invalid%2Freturn"}, nil
	case "jsapi":
		return adapter.CreateResult{MaterialType: "jsapi", Material: `{"appId":"wxsynthetic240","timeStamp":"1793680000","nonceStr":"synthetic240","package":"prepay_id=synthetic240","signType":"MD5","paySign":"synthetic-only-signature"}`}, nil
	default:
		return adapter.CreateResult{MaterialType: "app", Material: `{"appid":"wxsynthetic240","partnerid":"synthetic240","prepayid":"synthetic240","package":"Sign=WXPay","noncestr":"synthetic240","timestamp":"1793680000","sign":"synthetic-only-signature"}`}, nil
	}
}
func (p *wechatHTTP240Provider) Query(context.Context, adapter.QueryRequest) (adapter.QueryResult, error) {
	p.queries.Add(1)
	return adapter.QueryResult{}, adapter.ErrUnsupported
}

func TestWechatBrowser240OriginalMethodDesktopQRAndMobileContinuation(t *testing.T) {
	for _, scenario := range []string{"native", "h5", "jsapi", "app", "ported-h5"} {
		t.Run(scenario, func(t *testing.T) {
			method := strings.TrimPrefix(scenario, "ported-")
			f := channel168Local(t)
			p := &wechatHTTP240Provider{channel168Provider: &channel168Provider{app: "wx-browser240"}}
			f.s.ProviderFlow.Providers[7] = p
			if strings.HasPrefix(scenario, "ported-") {
				f.s.ProviderFlow.Providers[7] = &wechatPorted240Provider{p}
			}
			params := compat.Params{"pid": "1", "out_trade_no": "wechat240-" + method, "type": "wxpay", "method": method, "channel_id": "7", "money": "66.51", "name": "synthetic240", "clientip": "192.0.2.1", "sub_openid": "synthetic-buyer240", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
			params["sign"] = compat.SignMD5(params, "test-secret")
			values := url.Values{}
			for k, v := range params {
				values.Set(k, v)
			}
			req := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, req)
			var result map[string]any
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || result["code"] != float64(1) {
				t.Fatalf("create: %s", w.Body.String())
			}
			o, e := f.s.Store.Order(t.Context(), 1, params["out_trade_no"])
			if e != nil {
				t.Fatal(e)
			}
			before, e := f.s.Store.ProviderAttempt(t.Context(), o.TradeNo)
			if e != nil {
				t.Fatal(e)
			}
			target := f.s.checkoutURL(o.TradeNo, "checkout")
			get := func(target, ua string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", target, nil)
				r.Header.Set("User-Agent", ua)
				w := httptest.NewRecorder()
				f.s.Handler().ServeHTTP(w, r)
				return w
			}
			desktop := get(target, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/140")
			if scenario == "ported-h5" {
				if desktop.Code != 200 || strings.Contains(desktop.Body.String(), "browser-qr.png") || strings.Contains(desktop.Body.String(), "location.assign") || !strings.Contains(desktop.Body.String(), "继续支付") {
					t.Fatal("ported material must retain its existing manual rendering")
				}
				after, e := f.s.Store.ProviderAttempt(t.Context(), o.TradeNo)
				if e != nil || !reflect.DeepEqual(before, after) || p.sends.Load() != 1 || p.queries.Load() != 0 {
					t.Fatal("ported presentation changed original operation")
				}
				return
			}
			qrPath := "/browser-qr.png"
			if method == "native" {
				qrPath = "/qr.png"
			}
			if desktop.Code != 200 || !strings.Contains(desktop.Body.String(), qrPath) || !strings.Contains(desktop.Body.String(), "payment-status") {
				t.Fatalf("desktop missing original-order QR and local status: %d", desktop.Code)
			}
			qrTarget := strings.Replace(target, "?", qrPath+"?", 1)
			if qr := get(qrTarget, ""); qr.Code != 200 || qr.Header().Get("Content-Type") != "image/png" {
				t.Fatalf("QR unavailable: %d", qr.Code)
			}
			if method != "native" {
				originalURL, _ := url.Parse(target)
				originalQuery := originalURL.Query()
				originalQuery.Set("browser_launch", "1")
				originalURL.RawQuery = originalQuery.Encode()
				wantPNG, e := qrcode.Encode(originalURL.String(), qrcode.Medium, 320)
				if e != nil || !bytes.Equal(get(qrTarget, "").Body.Bytes(), wantPNG) {
					t.Fatal("continuation QR does not encode the same authenticated original checkout")
				}
			}
			if bad := get(strings.Replace(qrTarget, "token=", "token=x", 1), ""); bad.Code != 403 {
				t.Fatal("tampered QR accepted")
			}
			mobile := get(target, "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0) Mobile Safari/604.1")
			if mobile.Code != 200 {
				t.Fatalf("mobile %d", mobile.Code)
			}
			switch method {
			case "h5":
				if !strings.Contains(mobile.Body.String(), "location.assign") || mobile.Header().Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
					t.Fatal("H5 mobile must continue original URL with origin referrer")
				}
				inside := get(target, "Mozilla/5.0 (iPhone) Mobile MicroMessenger/8.0")
				if strings.Contains(inside.Body.String(), "location.assign") || !strings.Contains(inside.Body.String(), "浏览器") {
					t.Fatal("H5 wrongly auto launches inside WeChat")
				}
			case "jsapi":
				inside := get(target, "Mozilla/5.0 (iPhone) Mobile MicroMessenger/8.0")
				if !strings.Contains(inside.Body.String(), "WeixinJSBridgeReady") || !strings.Contains(inside.Body.String(), "getBrandWCPayRequest") {
					t.Fatal("JSAPI missing readiness-aware original invocation")
				}
				if strings.Contains(mobile.Body.String(), "WeixinJSBridge.invoke") {
					t.Fatal("JSAPI must not invoke outside WeChat")
				}
			case "native":
				if strings.Contains(mobile.Body.String(), "location.assign") {
					t.Fatal("native scan must not be converted into mobile H5 or fake app launch")
				}
			}
			after, e := f.s.Store.ProviderAttempt(t.Context(), o.TradeNo)
			if e != nil || !reflect.DeepEqual(before, after) || p.sends.Load() != 1 || p.queries.Load() != 0 {
				t.Fatal("presentation changed or resubmitted original payment", e)
			}
			originalApp := p.app
			p.app = "different-receiving-account240"
			if drift := get(target, "Mozilla/5.0 (iPhone) Mobile"); drift.Code != 503 {
				t.Fatal("unrestorable original receiving account must fail closed", drift.Code)
			}
			p.app = originalApp
			for _, status := range []string{"unknown", "closed", "paid"} {
				if _, e = f.p.Exec(t.Context(), `UPDATE orders SET status=$1 WHERE id=$2`, status, o.ID); e != nil {
					t.Fatal(e)
				}
				ended := get(target, "Mozilla/5.0 (iPhone) Mobile MicroMessenger/8.0")
				if ended.Code == http.StatusFound && status == "paid" {
					continue
				}
				if ended.Code != 200 || strings.Contains(ended.Body.String(), "location.assign") || strings.Contains(ended.Body.String(), "getBrandWCPayRequest") || strings.Contains(ended.Body.String(), "<img") {
					t.Fatalf("nonpending order still exposes payment: %s %d", status, ended.Code)
				}
				if qr := get(qrTarget, ""); qr.Code != http.StatusGone {
					t.Fatalf("nonpending QR %s %d", status, qr.Code)
				}
			}
		})
	}
}

func TestWechatBrowser240OAuthDesktopLeavesPhoneStateUnbound(t *testing.T) {
	f := channel168Local(t)
	p := &wechatHTTP240Provider{channel168Provider: &channel168Provider{app: "app168-seven"}}
	f.s.ProviderFlow.Providers[7] = p
	f.s.OAuthProviders = map[int64]adapter.WebOAuthProvider{7: &wxtransOAuth{}}
	params := compat.Params{"pid": "1", "out_trade_no": "wechat240-oauth", "type": "wxpay", "method": "jsapi", "oauth": "1", "channel_id": "7", "money": "1.00", "name": "synthetic OAuth240", "clientip": "192.0.2.1", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
	params["sign"] = compat.SignMD5(params, "test-secret")
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	request := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	created := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(created, request)
	var result map[string]any
	if json.Unmarshal(created.Body.Bytes(), &result) != nil || result["code"] != float64(1) {
		t.Fatalf("prepare OAuth: %s", created.Body.String())
	}
	order, e := f.s.Store.Order(t.Context(), 1, params["out_trade_no"])
	if e != nil {
		t.Fatal(e)
	}
	if order.Status != "created" || p.sends.Load() != 0 {
		t.Fatal("OAuth intent must precede provider creation")
	}
	if _, e = f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"pageordername","value":"1"}')`); e != nil {
		t.Fatal(e)
	}
	checkout := f.s.checkoutURL(order.TradeNo, "checkout")
	get := func(target, ua string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", target, nil)
		r.Header.Set("User-Agent", ua)
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	desktop := get(checkout, "Mozilla/5.0 Windows Chrome")
	if desktop.Code != 200 || !strings.Contains(desktop.Body.String(), "browser-qr.png") || desktop.Header().Get("Set-Cookie") != "" {
		t.Fatalf("desktop must show original intent QR without browser OAuth binding: %d", desktop.Code)
	}
	if strings.Contains(desktop.Body.String(), order.Name) || !strings.Contains(desktop.Body.String(), "onlinepay") {
		t.Fatal("OAuth QR exposed the hidden original goods name")
	}
	qr := get(strings.Replace(checkout, "?", "/browser-qr.png?", 1), "")
	if qr.Code != 200 || qr.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("intent QR missing %d", qr.Code)
	}
	var count int
	if e = f.p.QueryRow(t.Context(), `SELECT count(*) FROM oauth_states`).Scan(&count); e != nil || count != 0 {
		t.Fatal("desktop/QR prematurely issued phone OAuth state", count, e)
	}
	phone := get(checkout, "Mozilla/5.0 (iPhone) Mobile MicroMessenger/8.0")
	if phone.Code != 302 || !strings.HasPrefix(phone.Header().Get("Location"), "https://open.weixin.qq.com/") || phone.Header().Get("Set-Cookie") == "" {
		t.Fatalf("phone did not start original browser-bound OAuth: %d", phone.Code)
	}
	if e = f.p.QueryRow(t.Context(), `SELECT count(*) FROM oauth_states`).Scan(&count); e != nil || count != 1 || p.sends.Load() != 0 {
		t.Fatal("unexpected phone state or payment creation", count, e)
	}
}
