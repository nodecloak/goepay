package payment

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const browserCheckout239Gateway = "https://gateway.example.invalid:8443/custom/gateway.do"
const browserCheckout239Return = "https://pay.example.invalid/return?token=fresh%2F239&order=SYNTHETIC-239"

type browserCheckout239Transport struct{ t *testing.T }

func (transport browserCheckout239Transport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.t.Fatal("browser presentation must not call the provider or create a transaction")
	return nil, errors.New("unexpected provider I/O")
}

func browserCheckout239Canonical(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if key != "sign" && value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	return strings.Join(parts, "&")
}

func browserCheckout239Fixture(t *testing.T, key *rsa.PrivateKey, method, biz string, legacy bool) string {
	t.Helper()
	params := map[string]string{
		"app_id": "synthetic-browser239-app", "method": method,
		"format": "JSON", "charset": "utf-8", "sign_type": "RSA2", "version": "1.0",
		"timestamp": "2026-11-02 01:02:03", "biz_content": biz,
		"notify_url":     "https://notify.example.invalid/callback?tenant=239&channel=synthetic",
		"return_url":     "https://pay.example.invalid/return?token=expired",
		"app_auth_token": "synthetic-auth-token-239",
	}
	params["sign"] = independentSignature(t, browserCheckout239Canonical(params), key)
	var out strings.Builder
	if legacy {
		out.WriteString(`<form method="post" action="` + browserCheckout239Gateway + `">`)
	} else {
		out.WriteString(`<form method="post" accept-charset="UTF-8" action="` + browserCheckout239Gateway + `?charset=utf-8">`)
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out.WriteString(`<input type="hidden" name="` + key + `" value="` + html.EscapeString(params[key]) + `">`)
	}
	out.WriteString(`<button type="submit">支付宝支付</button></form>`)
	return out.String()
}

func browserCheckout239Fields(t *testing.T, material string) map[string]string {
	t.Helper()
	fields := map[string]string{}
	last := ""
	for _, match := range regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`).FindAllStringSubmatch(material, -1) {
		if match[1] <= last {
			t.Fatal("presentation inputs must be sorted and unique")
		}
		last = match[1]
		fields[match[1]] = html.UnescapeString(match[2])
	}
	return fields
}

func browserCheckout239JSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func TestAlipayBrowserCheckout239PreservesVerifiedBusinessAndProviderMethod(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	adapter, err := NewAlipay(AlipayConfig{
		AppID: "synthetic-browser239-app", PrivateKey: merchant, PublicKey: &platform.PublicKey,
		GatewayURL: browserCheckout239Gateway, HTTPClient: &http.Client{Transport: browserCheckout239Transport{t}},
		Now: func() time.Time { return time.Date(2026, 11, 3, 9, 10, 11, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	const bizBase = `{"out_trade_no":"SYNTHETIC-239","total_amount":"12.30","seller_id":"2088000000000239","subject":"中文 & <商品> \"test\"","body":"原商品详情","product_code":"FAST_INSTANT_TRADE_PAY","timeout_express":"30m","passback_params":"tenant%3D239%26order%3Dsynthetic","goods_detail":[{"goods_id":"SKU239","quantity":9007199254740993,"price":1.2300}],"extend_params":{"opaque":"keep","nullable":null},"settle_info":{"settle_detail_infos":[{"trans_in_type":"userId","trans_in":"2088000000000239","amount":"12.30"}]}}`
	// These cases catch an accidental wap->page switch, mobile QR overrides,
	// dropped business fields, numeric rounding, or a signature over stale bytes.
	for _, tc := range []struct {
		name, method, qr string
		desktop, legacy  bool
	}{
		{"desktop page adds QR", "page", "", true, false},
		{"desktop page replaces QR in legacy form", "page", `,"qr_pay_mode":"2","qrcode_width":"100"`, true, true},
		{"desktop wap stays wap", "wap", "", true, false},
		{"mobile page stays page", "page", "", false, true},
		{"mobile page keeps its QR settings", "page", `,"qr_pay_mode":"2","qrcode_width":"100"`, false, false},
		{"mobile wap stays wap", "wap", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			biz := strings.TrimSuffix(bizBase, "}") + tc.qr + "}"
			if tc.method == "wap" {
				biz = strings.Replace(biz, "FAST_INSTANT_TRADE_PAY", "QUICK_WAP_WAY", 1)
			}
			providerMethod := "alipay.trade." + tc.method + ".pay"
			material := browserCheckout239Fixture(t, merchant, providerMethod, biz, tc.legacy)
			beforeBytes := []byte(material)
			beforeHash := sha256.Sum256(beforeBytes)
			before := browserCheckout239Fields(t, material)
			browserCheckout239JSON(t, before["biz_content"])
			got, err := adapter.PrepareBrowserCheckout(material, "SYNTHETIC-239", browserCheckout239Return, tc.desktop)
			if err != nil {
				t.Fatal(err)
			}
			if material != string(beforeBytes) || sha256.Sum256([]byte(material)) != beforeHash {
				t.Fatal("changed persisted original material bytes")
			}
			if got.Method != tc.method || got.GatewayOrigin != "https://gateway.example.invalid:8443" {
				t.Fatalf("incorrect verified browser metadata: method=%q origin=%q", got.Method, got.GatewayOrigin)
			}
			if !strings.HasPrefix(got.HTML, `<form method="post" accept-charset="UTF-8" action="`+browserCheckout239Gateway+`?charset=utf-8">`) || !strings.HasSuffix(got.HTML, `<button type="submit">支付宝支付</button></form>`) {
				t.Fatal("presentation must use the existing canonical UTF-8 gateway form")
			}
			after := browserCheckout239Fields(t, got.HTML)
			if len(after) != len(before) || after["method"] != providerMethod || after["return_url"] != browserCheckout239Return || after["timestamp"] != "2026-11-03 17:10:11" || after["sign"] == before["sign"] {
				t.Fatal("fresh presentation changed provider method or failed to refresh signed return/timestamp")
			}
			for key, value := range before {
				if key != "biz_content" && key != "return_url" && key != "timestamp" && key != "sign" && after[key] != value {
					t.Fatalf("changed protected signed field %q", key)
				}
			}
			beforeBiz := browserCheckout239JSON(t, before["biz_content"])
			afterBiz := browserCheckout239JSON(t, after["biz_content"])
			if tc.desktop && tc.method == "page" {
				if afterBiz["qr_pay_mode"] != "4" || afterBiz["qrcode_width"] != "230" {
					t.Fatal("desktop page must request the provider's embedded QR presentation")
				}
				delete(beforeBiz, "qr_pay_mode")
				delete(beforeBiz, "qrcode_width")
				delete(afterBiz, "qr_pay_mode")
				delete(afterBiz, "qrcode_width")
			} else if after["biz_content"] != before["biz_content"] {
				t.Fatal("wap/mobile must retain the exact original signed business JSON")
			}
			if !reflect.DeepEqual(afterBiz, beforeBiz) {
				t.Fatal("lost or changed original amount, seller, goods, callback, or other business value")
			}
			signature, err := base64.StdEncoding.Strict().DecodeString(after["sign"])
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(browserCheckout239Canonical(after)))
			if err := rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
				t.Fatal("independent RSA2 verification of rendered form failed", err)
			}
		})
	}
}

func TestAlipayBrowserCheckout239RejectsInvalidOriginalMaterial(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	adapter, err := NewAlipay(AlipayConfig{AppID: "synthetic-browser239-app", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: browserCheckout239Gateway, HTTPClient: &http.Client{Transport: browserCheckout239Transport{t}}})
	if err != nil {
		t.Fatal(err)
	}
	const biz = `{"out_trade_no":"SYNTHETIC-239","total_amount":"12.30","seller_id":"2088000000000239","subject":"synthetic"}`
	material := browserCheckout239Fixture(t, merchant, "alipay.trade.page.pay", biz, false)
	for _, tc := range []struct {
		name, material, trade, returnURL string
	}{
		{"wrong signature", strings.Replace(material, "12.30", "13.30", 1), "SYNTHETIC-239", browserCheckout239Return},
		{"wrong application", strings.Replace(material, "synthetic-browser239-app", "other-app", 1), "SYNTHETIC-239", browserCheckout239Return},
		{"wrong gateway", strings.Replace(material, "gateway.example.invalid", "attacker.example.invalid", 1), "SYNTHETIC-239", browserCheckout239Return},
		{"wrong order", material, "OTHER-ORDER", browserCheckout239Return},
		{"duplicate hidden field", strings.Replace(material, `<button`, `<input type="hidden" name="charset" value="utf-8"><button`, 1), "SYNTHETIC-239", browserCheckout239Return},
		{"duplicate business field", browserCheckout239Fixture(t, merchant, "alipay.trade.page.pay", strings.TrimSuffix(biz, "}")+`,"total_amount":"99.99"}`, false), "SYNTHETIC-239", browserCheckout239Return},
		{"unsupported provider method", browserCheckout239Fixture(t, merchant, "alipay.trade.app.pay", biz, false), "SYNTHETIC-239", browserCheckout239Return},
		{"unsafe return URL", material, "SYNTHETIC-239", "javascript:alert(1)"},
	} {
		for _, desktop := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{true: "/desktop", false: "/mobile"}[desktop], func(t *testing.T) {
				before := sha256.Sum256([]byte(tc.material))
				got, err := adapter.PrepareBrowserCheckout(tc.material, tc.trade, tc.returnURL, desktop)
				if err == nil || got != (BrowserCheckoutPresentation{}) {
					t.Fatal("invalid original material must fail without presentation metadata")
				}
				if sha256.Sum256([]byte(tc.material)) != before {
					t.Fatal("changed invalid original material")
				}
			})
		}
	}
	var nilAdapter *Alipay
	if _, err := nilAdapter.PrepareBrowserCheckout(material, "SYNTHETIC-239", browserCheckout239Return, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil adapter: %v", err)
	}
}
