package contract_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestV2WebPreservesHTMLAndQRMaterialWhileJumpStaysProtected(t *testing.T) {
	s, _, key := fixture(t)
	apiKey := "0123456789abcdef0123456789abcdef"
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "wxapp", "mch_id": "mch", "code_url": "weixin://local-native"}
		p["sign"], _ = adapter.SignWechatV2(p, apiKey, "MD5")
		w.Write(providerXML(p))
	}))
	defer up.Close()
	wx, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "wxapp", MerchantID: "mch", APIKey: apiKey, Methods: []string{"native"}, BaseURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"page"}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: wx, 8: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{8}, 32)
	s.MerchantChannels = map[int64][]int64{2: {7, 8}}
	for _, kind := range []string{"wxpay", "alipay"} {
		for _, method := range []string{"web", "jump"} {
			p := compat.Params{"pid": "2", "type": kind, "method": method, "out_trade_no": kind + "-" + method, "name": "title <tag>", "money": "1.00", "sign_type": "RSA", "timestamp": fmt.Sprint(time.Now().Unix())}
			p["sign"], _ = compat.SignRSA(p, key)
			v := url.Values{}
			for k, x := range p {
				v.Set(k, x)
			}
			_, out := call(t, s.Handler(), "POST", "/api/pay/create", v)
			if out["code"] != float64(0) {
				t.Fatal(out)
			}
			a, e := s.Store.ProviderAttempt(context.Background(), out["trade_no"].(string))
			if e != nil {
				t.Fatal(e)
			}
			if method == "web" {
				want := "html"
				if kind == "wxpay" {
					want = "qrcode"
				}
				if out["pay_type"] != want {
					t.Fatalf("%s web material replaced with wrapper: type=%v", kind, out["pay_type"])
				}
				if kind == "wxpay" {
					if out["pay_info"] != a.Material {
						t.Fatal("native QR material changed")
					}
				} else {
					material, ok := out["pay_info"].(string)
					verified, err := ali.PrepareCheckoutHTML(material, a.TradeNo)
					if !ok || err != nil || verified != material {
						t.Fatal("returned HTML is not the original adapter's signed order form", err)
					}
					inputs := regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)
					fields := func(form string) map[string]string {
						values := map[string]string{}
						for _, match := range inputs.FindAllStringSubmatch(form, -1) {
							values[html.UnescapeString(match[1])] = html.UnescapeString(match[2])
						}
						return values
					}
					before, after := fields(a.Material), fields(material)
					fieldCount := len(before)
					if _, exists := before["return_url"]; !exists {
						fieldCount++ // Optional merchant return was absent in the saved form.
					}
					returned, err := url.Parse(after["return_url"])
					if err != nil || fieldCount != len(after) || returned.Scheme != "https" || returned.Host != "pay.example.invalid" || returned.Path != "/checkout/"+a.TradeNo+"/return" || returned.User != nil || returned.Fragment != "" {
						t.Fatal("returned form changed its fields or original-order return URL")
					}
					query := returned.Query()
					expires, err := strconv.ParseInt(query.Get("expires"), 10, 64)
					mac := hmac.New(sha256.New, s.CheckoutKey)
					fmt.Fprintf(mac, "goepay-checkout-v1\n%s\nreturn\n%s", a.TradeNo, query.Get("expires"))
					if err != nil || len(query) != 2 || len(query["token"]) != 1 || len(query["expires"]) != 1 || expires <= time.Now().Unix() || expires > time.Now().Add(2*time.Hour).Unix() || query.Get("token") != base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) {
						t.Fatal("returned form did not use an authenticated original-order return token")
					}
					for field, value := range before {
						if field != "timestamp" && field != "return_url" && field != "sign" && after[field] != value {
							t.Fatal("returned form changed a protected signed field", field)
						}
					}
					saved, err := s.Store.ProviderAttempt(context.Background(), a.TradeNo)
					if err != nil || saved.Material != a.Material {
						t.Fatal("refresh modified immutable saved payment material", err)
					}
				}
			} else {
				raw, _ := out["pay_info"].(string)
				if out["pay_type"] != "jump" || !strings.HasPrefix(raw, "https://pay.example.invalid/checkout/") {
					t.Fatal("jump did not remain protected", out)
				}
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, httptest.NewRequest("GET", raw, nil))
				if w.Code != 200 {
					t.Fatalf("protected checkout status=%d", w.Code)
				}
				if e = s.Store.MarkPaid(context.Background(), a.TradeNo, "test-paid-display:"+a.TradeNo); e != nil {
					t.Fatal(e)
				}
				w = httptest.NewRecorder()
				s.Handler().ServeHTTP(w, httptest.NewRequest("GET", raw, nil))
				if w.Code != 200 || !strings.Contains(w.Body.String(), "已确认支付") || strings.Contains(w.Body.String(), "<form") || strings.Contains(w.Body.String(), "<img") {
					t.Fatalf("paid checkout continued offering payment material: status=%d", w.Code)
				}
				if kind == "wxpay" {
					u, _ := url.Parse(raw)
					u.Path += "/qr.png"
					w = httptest.NewRecorder()
					s.Handler().ServeHTTP(w, httptest.NewRequest("GET", u.String(), nil))
					if w.Code != 410 {
						t.Fatalf("paid QR could be regenerated status=%d", w.Code)
					}
				}
			}
			result := compat.Params{}
			for k, x := range out {
				result[k] = fmt.Sprint(x)
			}
			if !compat.VerifyRSAWithProfile(result, result["sign"], &key.PublicKey, compat.ProfileSDKV2) {
				t.Fatal("material response signature incompatible")
			}
		}
	}
}

func TestV2JumpForAppOnlyProductKeepsProtectedCheckout(t *testing.T) {
	s, _, key := fixture(t)
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"app"}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{8: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{8}, 32)
	s.MerchantChannels = map[int64][]int64{2: {8}}
	p := compat.Params{"pid": "2", "type": "alipay", "method": "jump", "out_trade_no": "app-jump", "name": "APP jump", "money": "1.00", "sign_type": "RSA", "timestamp": fmt.Sprint(time.Now().Unix())}
	p["sign"], _ = compat.SignRSA(p, key)
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	_, out := call(t, s.Handler(), "POST", "/api/pay/create", v)
	if out["code"] != float64(0) || out["pay_type"] != "jump" || !strings.HasPrefix(fmt.Sprint(out["pay_info"]), "https://pay.example.invalid/checkout/") {
		t.Fatal("APP jump exposed SDK material", out)
	}
	a, e := s.Store.ProviderAttempt(context.Background(), fmt.Sprint(out["trade_no"]))
	if e != nil || a.Method != "app" || a.MaterialType != "app" {
		t.Fatal(a, e)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", out["pay_info"].(string), nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "alipays://") {
		t.Fatal("APP checkout unavailable", w.Code, w.Body.String())
	}
}
