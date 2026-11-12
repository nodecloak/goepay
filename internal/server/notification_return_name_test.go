package server

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"goepay/internal/compat"
)

// Every synchronous return must use the same name policy as the durable
// notification. Only in-process synthetic providers are used in this test.
func TestPaymentReturnAppliesNotificationNamePolicy(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		t.Run(version, func(t *testing.T) {
			f := notificationReturn200Local(t)
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Fatal(err)
			}
			f.s.PlatformKey = key
			order := f.order(t, "return-name-"+version, version, "https://merchant.example.invalid/return?tenant=original", false)
			if _, err = f.s.ProviderFlow.Submit(t.Context(), order.TradeNo, 7, "native", "192.0.2.1", ""); err != nil {
				t.Fatal(err)
			}
			if err = f.s.Store.MarkPaid(t.Context(), order.TradeNo, "synthetic-return-name-"+version); err != nil {
				t.Fatal(err)
			}
			if _, err = f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"notifyordername","value":"1"}')`); err != nil {
				t.Fatal(err)
			}
			for _, hide := range []bool{true, false} {
				value, expectedName := "1", "product"
				if !hide {
					value, expectedName = "0", order.Name
				}
				if _, err = f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_set(body,'{value}',to_jsonb($1::text)) WHERE resource='settings' AND body->>'key'='notifyordername'`, value); err != nil {
					t.Fatal(err)
				}
				before := f.effects(t)
				for _, path := range []string{"checkout", "merchant", "admin"} {
					t.Run(path+"/hide="+value, func(t *testing.T) {
						var target *url.URL
						if path == "checkout" {
							w := httptest.NewRecorder()
							f.s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, f.s.ProviderReturnURL(order.TradeNo), nil))
							if w.Code != http.StatusFound {
								t.Fatalf("checkout return status=%d", w.Code)
							}
							target, err = url.Parse(w.Header().Get("Location"))
							if err != nil {
								t.Fatal(err)
							}
						} else {
							token, csrf := f.merchantToken, f.merchantCSRF
							if path == "admin" {
								token, csrf = f.token, f.csrf
							}
							target = notificationReturn200URL(t, f.request(path, token, csrf, http.MethodPost, order.TradeNo+"/return-notification", `{}`))
						}
						params, err := compat.Parse(target.Query())
						if err != nil || params["name"] != expectedName || params["tenant"] != "original" || params["money"] != "1.23" || params["trade_no"] != order.TradeNo {
							t.Fatalf("return name=%q want=%q; original order fields must stay intact", params["name"], expectedName)
						}
						valid := compat.VerifyMD5(params, "test-secret", params["sign"])
						if version == "v2" {
							valid = compat.VerifyRSAWithProfile(params, params["sign"], &key.PublicKey, compat.ProfileSDKV2)
						}
						if !valid || f.effects(t) != before {
							t.Fatal("return signature or original async/funds state changed")
						}
					})
				}
			}
		})
	}
}
