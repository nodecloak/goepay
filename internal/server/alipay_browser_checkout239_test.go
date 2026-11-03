package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"goepay/internal/channelconfig"
	"goepay/internal/compat"
)

func TestAlipayBrowser239RenderingPreservesOriginalOrder(t *testing.T) {
	f := channel168Local(t)
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	providers, e := channelconfig.Build(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "alipay", AppTypes: []string{"1", "2"}, Config: map[string]string{"appid": "synthetic-browser239", "appsecret": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), "appkey": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub}))}}}}, true)
	if e != nil {
		t.Fatal(e)
	}
	f.s.ProviderFlow.Providers[7] = providers[7]
	for _, method := range []string{"page", "wap"} {
		t.Run(method, func(t *testing.T) {
			p := compat.Params{"pid": "1", "out_trade_no": "browser239-" + method, "type": "alipay", "name": "synthetic browser item", "money": "66.50", "channel_id": "7", "method": method, "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
			p["sign"] = compat.SignMD5(p, "test-secret")
			form := url.Values{}
			for k, v := range p {
				form.Set(k, v)
			}
			r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, r)
			var created map[string]any
			if json.Unmarshal(w.Body.Bytes(), &created) != nil || created["code"] != float64(1) {
				t.Fatal("synthetic page/wap create failed")
			}
			order, e := f.s.Store.Order(t.Context(), 1, p["out_trade_no"])
			if e != nil {
				t.Fatal(e)
			}
			before, e := f.s.Store.ProviderAttempt(t.Context(), order.TradeNo)
			if e != nil {
				t.Fatal(e)
			}
			checkout := f.s.checkoutURL(order.TradeNo, "checkout")
			get := func(path, ua string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", path, nil)
				r.Header.Set("User-Agent", ua)
				w := httptest.NewRecorder()
				f.s.Handler().ServeHTTP(w, r)
				return w
			}
			desktop := get(checkout, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130.0.0.0 Safari/537.36")
			if desktop.Code != 200 {
				t.Fatalf("desktop status=%d", desktop.Code)
			}
			body := desktop.Body.String()
			if method == "page" {
				if !strings.Contains(body, `title="支付宝支付二维码"`) || !strings.Contains(body, `target="alipay-cashier"`) {
					t.Fatal("desktop page lacks embedded original Alipay QR cashier")
				}
				if !strings.Contains(desktop.Header().Get("Content-Security-Policy"), "frame-src") {
					t.Fatal("embedded cashier lacks explicit frame policy")
				}
				for _, directive := range strings.Split(desktop.Header().Get("Content-Security-Policy"), ";") {
					if strings.HasPrefix(strings.TrimSpace(directive), "form-action ") && (!strings.Contains(directive, "https://*.alipay.com") || !strings.Contains(directive, "'self'") || !strings.Contains(directive, "https://merchant.example.invalid")) {
						t.Fatal("browser submission policy blocks the original Alipay redirect/return chain")
					}
				}
				match := regexp.MustCompile(`name="biz_content" value="([^"]+)"`).FindStringSubmatch(body)
				var biz map[string]any
				if len(match) != 2 || json.Unmarshal([]byte(html.UnescapeString(match[1])), &biz) != nil || biz["qr_pay_mode"] != "4" || biz["qrcode_width"] != "230" || biz["out_trade_no"] != order.TradeNo || biz["total_amount"] != "66.50" {
					t.Fatal("desktop QR presentation changed or omitted original business identity")
				}
			} else if !strings.Contains(body, "browser-qr.png") || !strings.Contains(body, "支付二维码") {
				t.Fatal("desktop wap lacks same-order mobile continuation QR")
			}
			for _, ua := range []string{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) Mobile/15E148 Safari/604.1", "Mozilla/5.0 (Linux; Android 15) AppleWebKit/537.36 Mobile Safari/537.36 AlipayClient/10.6.0", "Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X) Mobile/15E148"} {
				mobile := get(checkout, ua)
				if mobile.Code != 200 || strings.Contains(mobile.Body.String(), "<iframe") || !strings.Contains(mobile.Body.String(), "alipay-browser-form") || !strings.Contains(mobile.Body.String(), "HTMLFormElement.prototype.submit.call") {
					t.Fatal("mobile must submit the original signed form without desktop QR intermediary")
				}
				want := "alipay.trade." + method + ".pay"
				if !strings.Contains(mobile.Body.String(), want) {
					t.Fatal("mobile changed the original payment method")
				}
			}
			qrURL := strings.Replace(checkout, "?", "/browser-qr.png?", 1)
			qr := get(qrURL, "")
			if qr.Code != 200 || qr.Header().Get("Content-Type") != "image/png" {
				t.Fatal("same-order browser QR unavailable")
			}
			bad := get(strings.Replace(qrURL, "token=", "token=x", 1), "")
			if bad.Code != http.StatusForbidden {
				t.Fatal("browser QR accepted tampered checkout token")
			}
			after, e := f.s.Store.ProviderAttempt(t.Context(), order.TradeNo)
			if e != nil {
				t.Fatal(e)
			}
			if before.Material != after.Material || before.Method != after.Method || before.ChannelID != after.ChannelID || before.State != after.State {
				t.Fatal("browser presentation rewrote original upstream operation")
			}
			if _, e = f.p.Exec(t.Context(), `UPDATE orders SET status='unknown' WHERE id=$1`, order.ID); e != nil {
				t.Fatal(e)
			}
			uncertain := get(checkout, "Mozilla/5.0 (iPhone) Mobile")
			if uncertain.Code != 200 || strings.Contains(uncertain.Body.String(), "<form") || strings.Contains(uncertain.Body.String(), "<iframe") || strings.Contains(uncertain.Body.String(), "订单已关闭") || !strings.Contains(uncertain.Body.String(), "核实") {
				t.Fatal("uncertain original order must stay read-only without being misreported closed")
			}
			if _, e = f.p.Exec(t.Context(), `UPDATE orders SET status='closed' WHERE id=$1`, order.ID); e != nil {
				t.Fatal(e)
			}
			closed := get(checkout, "Mozilla/5.0 (iPhone) Mobile")
			if closed.Code != 200 || strings.Contains(closed.Body.String(), "<form") || strings.Contains(closed.Body.String(), "<iframe") || strings.Contains(closed.Body.String(), "prototype.submit") || !strings.Contains(closed.Body.String(), "关闭") {
				t.Fatal("closed original order still exposes automatic payment material")
			}
			if closedQR := get(qrURL, ""); closedQR.Code != http.StatusGone {
				t.Fatal("closed order still exposes mobile continuation QR")
			}
		})
	}
}
