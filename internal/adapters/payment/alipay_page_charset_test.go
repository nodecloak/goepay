package payment

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// Verify the rendered browser submission independently of the signing helper:
// charset belongs to both the signed fields and the gateway URL, like Pro's
// pageExecute. Everything here uses generated keys and synthetic orders.
func TestAlipayPageAndWapFormCharsetAndSignedPayload(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	ali, err := NewAlipay(AlipayConfig{AppID: "synthetic-app", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: "https://gateway.example.invalid/gateway.do", Methods: []string{"page", "wap"}})
	if err != nil {
		t.Fatal(err)
	}
	actionRE := regexp.MustCompile(`<form\s+[^>]*action="([^"]+)"`)
	inputRE := regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)
	for _, method := range []string{"page", "wap"} {
		t.Run(method, func(t *testing.T) {
			result, err := ali.Create(context.Background(), CreateRequest{TradeNo: "SYNTHETIC-1", Method: method, Subject: `中文 & "test"`, Amount: 123, NotifyURL: "https://notify.example.invalid/cb", ReturnURL: "https://return.example.invalid/cb"})
			if err != nil || result.MaterialType != "html" {
				t.Fatal("form create failed", err)
			}
			action := actionRE.FindStringSubmatch(result.Material)
			if len(action) != 2 {
				t.Fatal("missing form action")
			}
			u, err := url.Parse(html.UnescapeString(action[1]))
			if err != nil {
				t.Fatal(err)
			}
			if u.Scheme != "https" || u.Host != "gateway.example.invalid" || u.Path != "/gateway.do" || u.RawQuery != "charset=utf-8" {
				t.Fatal("gateway must receive the signed charset in its URL query")
			}
			if !strings.Contains(result.Material, `accept-charset="UTF-8"`) {
				t.Fatal("form must force UTF-8 browser encoding")
			}
			params := map[string]string{}
			for _, input := range inputRE.FindAllStringSubmatch(result.Material, -1) {
				k, v := html.UnescapeString(input[1]), html.UnescapeString(input[2])
				if _, dup := params[k]; dup {
					t.Fatal("duplicate signed field", k)
				}
				params[k] = v
			}
			if params["charset"] != "utf-8" || params["sign_type"] != "RSA2" {
				t.Fatal("missing signed charset or sign type")
			}
			wantMethod := map[string]string{"page": "alipay.trade.page.pay", "wap": "alipay.trade.wap.pay"}[method]
			if params["method"] != wantMethod || !strings.Contains(params["biz_content"], "中文") {
				t.Fatal("incorrect browser payment parameters")
			}
			signature, err := base64.StdEncoding.DecodeString(params["sign"])
			if err != nil {
				t.Fatal(err)
			}
			delete(params, "sign")
			keys := make([]string, 0, len(params))
			for k, v := range params {
				if v != "" {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, k+"="+params[k])
			}
			digest := sha256.Sum256([]byte(strings.Join(parts, "&")))
			if err = rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
				t.Fatal("form signature does not cover the exact UTF-8 fields", err)
			}
		})
	}
}

func TestAlipayRefreshCheckoutHTMLReissuesOnlyReturnAndTimestamp(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	now := time.Date(2026, 10, 30, 11, 0, 0, 0, time.UTC)
	ali, err := NewAlipay(AlipayConfig{AppID: "synthetic-app", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: "https://gateway.example.invalid/gateway.do", Methods: []string{"page", "wap"}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	refresh, ok := any(ali).(interface {
		RefreshCheckoutHTML(string, string, string) (string, error)
	})
	if !ok {
		t.Fatal("Alipay adapter must refresh expired checkout return material")
	}
	inputRE := regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)
	fields := func(material string) map[string]string {
		out := map[string]string{}
		for _, m := range inputRE.FindAllStringSubmatch(material, -1) {
			out[html.UnescapeString(m[1])] = html.UnescapeString(m[2])
		}
		return out
	}
	for _, method := range []string{"page", "wap"} {
		t.Run(method, func(t *testing.T) {
			now = time.Date(2026, 10, 30, 11, 0, 0, 0, time.UTC)
			r, err := ali.Create(context.Background(), CreateRequest{TradeNo: "SYNTHETIC-FRESH", Method: method, Subject: `中文 & "test"`, Amount: 123, NotifyURL: "https://notify.example.invalid/cb", ReturnURL: "https://pay.example.invalid/return?token=expired"})
			if err != nil {
				t.Fatal(err)
			}
			newPrefix := `<form method="post" accept-charset="UTF-8" action="https://gateway.example.invalid/gateway.do?charset=utf-8">`
			oldPrefix := `<form method="post" action="https://gateway.example.invalid/gateway.do">`
			old := oldPrefix + strings.TrimPrefix(r.Material, newPrefix)
			before := fields(old)
			originalInputs := map[string]string{}
			for _, m := range inputRE.FindAllStringSubmatch(old, -1) {
				originalInputs[html.UnescapeString(m[1])] = m[0]
			}
			now = now.Add(2 * time.Hour)
			freshReturn := "https://pay.example.invalid/return?token=new%2Fsynthetic"
			out, err := refresh.RefreshCheckoutHTML(old, "SYNTHETIC-FRESH", freshReturn)
			if err != nil || !strings.HasPrefix(out, newPrefix) {
				t.Fatal("could not refresh saved checkout", err)
			}
			after := fields(out)
			for _, m := range inputRE.FindAllStringSubmatch(out, -1) {
				key := html.UnescapeString(m[1])
				if key != "return_url" && key != "timestamp" && key != "sign" && m[0] != originalInputs[key] {
					t.Fatal("changed protected field bytes", key)
				}
			}
			if after["return_url"] != freshReturn || after["timestamp"] == before["timestamp"] || after["sign"] == before["sign"] {
				t.Fatal("fresh return, timestamp and signature required")
			}
			if len(after) != len(before) {
				t.Fatal("changed the signed parameter set")
			}
			for key, value := range before {
				if key != "return_url" && key != "timestamp" && key != "sign" && after[key] != value {
					t.Fatal("changed a protected signed field", key)
				}
			}
			sig, err := base64.StdEncoding.DecodeString(after["sign"])
			if err != nil {
				t.Fatal(err)
			}
			delete(after, "sign")
			keys := make([]string, 0, len(after))
			for key, value := range after {
				if value != "" {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, key := range keys {
				parts = append(parts, key+"="+after[key])
			}
			digest := sha256.Sum256([]byte(strings.Join(parts, "&")))
			if err = rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
				t.Fatal("fresh form signature invalid", err)
			}
			if _, err = refresh.RefreshCheckoutHTML(strings.Replace(old, "https://notify.example.invalid/cb", "https://notify.example.invalid/altered", 1), "SYNTHETIC-FRESH", freshReturn); err == nil {
				t.Fatal("accepted altered original material")
			}
			if _, err = refresh.RefreshCheckoutHTML(old, "OTHER", freshReturn); err == nil {
				t.Fatal("accepted another order")
			}
			if _, err = refresh.RefreshCheckoutHTML(old, "SYNTHETIC-FRESH", "javascript:alert(1)"); err == nil {
				t.Fatal("accepted unsafe return URL")
			}
		})
	}
}

func TestAlipayPrepareCheckoutHTMLValidatesAndUpgradesOnlyItsOwnSignedMaterial(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	ali, err := NewAlipay(AlipayConfig{AppID: "synthetic-app", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: "https://gateway.example.invalid/gateway.do", Methods: []string{"page", "wap"}})
	if err != nil {
		t.Fatal(err)
	}
	prepare, ok := any(ali).(interface {
		PrepareCheckoutHTML(string, string) (string, error)
	})
	if !ok {
		t.Fatal("Alipay adapter must validate and upgrade its saved checkout HTML")
	}
	for _, method := range []string{"page", "wap"} {
		t.Run(method, func(t *testing.T) {
			r, err := ali.Create(context.Background(), CreateRequest{TradeNo: "SYNTHETIC-2", Method: method, Subject: `中文 & "test"`, Amount: 123, NotifyURL: "https://notify.example.invalid/cb"})
			if err != nil {
				t.Fatal(err)
			}
			newPrefix := `<form method="post" accept-charset="UTF-8" action="https://gateway.example.invalid/gateway.do?charset=utf-8">`
			oldPrefix := `<form method="post" action="https://gateway.example.invalid/gateway.do">`
			if !strings.HasPrefix(r.Material, newPrefix) {
				t.Fatal("unexpected test fixture")
			}
			old := oldPrefix + strings.TrimPrefix(r.Material, newPrefix)
			out, err := prepare.PrepareCheckoutHTML(old, "SYNTHETIC-2")
			if err != nil || out != r.Material {
				t.Fatal("legacy signed form was not upgraded losslessly", err)
			}
			out, err = prepare.PrepareCheckoutHTML(r.Material, "SYNTHETIC-2")
			if err != nil || out != r.Material {
				t.Fatal("new form was not accepted unchanged", err)
			}
			for name, bad := range map[string]string{
				"gateway":         strings.Replace(old, "gateway.example.invalid", "attacker.example.invalid", 1),
				"signed field":    strings.Replace(old, `name="charset" value="utf-8"`, `name="charset" value="gbk"`, 1),
				"signature":       strings.Replace(old, "https://notify.example.invalid/cb", "https://notify.example.invalid/changed", 1),
				"duplicate field": strings.Replace(old, `<button`, `<input type="hidden" name="charset" value="utf-8"><button`, 1),
				"extra markup":    strings.Replace(old, `<button`, `<script>alert(1)</script><button`, 1),
			} {
				if _, err = prepare.PrepareCheckoutHTML(bad, "SYNTHETIC-2"); err == nil {
					t.Fatal("accepted altered material", name)
				}
			}
			if _, err = prepare.PrepareCheckoutHTML(old, "OTHER-ORDER"); err == nil {
				t.Fatal("accepted another order's material")
			}
		})
	}
}
