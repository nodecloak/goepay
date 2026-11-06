package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"goepay/internal/compat"
	"goepay/internal/paymentdefend"
)

// These providers are in-process test doubles. The first request creates only
// the original prepared OAuth intent; no real provider or funds are involved.
func TestPaymentDefend206OAuthContinuationCannotBypassLaterPolicy(t *testing.T) {
	for _, kind := range []string{"web", "mini"} {
		t.Run(kind, func(t *testing.T) {
			f, factory, _ := subchannel168BProductLocal(t)
			var err error
			f.s.PaymentDefend206, err = paymentdefend.NewStore(f.p, make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}
			method, extra := "jsapi", compat.Params{"oauth": "1"}
			if kind == "mini" {
				method, extra = "applet", nil
			}
			no := "defend206-continuation-" + kind
			if out := subchannel168BProductPay(t, f, no, method, extra); out["code"] != float64(1) {
				t.Fatal("off policy did not prepare the original intent", out)
			}
			o, err := f.s.Store.Order(context.Background(), 1, no)
			if err != nil {
				t.Fatal(err)
			}
			checkout := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(checkout, httptest.NewRequest(http.MethodGet, f.s.checkoutURL(o.TradeNo, "checkout"), nil))
			p := factory.providers["products-v1"]
			if p == nil || p.creates.Load() != 0 {
				t.Fatal("prepared intent unexpectedly created a payment")
			}
			var request *http.Request
			if kind == "web" {
				if checkout.Code != http.StatusFound {
					t.Fatal("original OAuth entry unavailable", checkout.Code)
				}
				u, err := url.Parse(checkout.Header().Get("Location"))
				if err != nil {
					t.Fatal(err)
				}
				callback := u.Query().Get("redirect_uri") + "?" + url.Values{"state": {u.Query().Get("state")}, "auth_code": {"code:products-v1"}, "app_id": {"products-v1"}}.Encode()
				request = httptest.NewRequest(http.MethodGet, callback, nil)
				for _, cookie := range checkout.Result().Cookies() {
					request.AddCookie(cookie)
				}
			} else {
				if checkout.Code != http.StatusOK || p.completeURL == "" {
					t.Fatal("original mini entry unavailable", checkout.Code)
				}
				request = httptest.NewRequest(http.MethodPost, p.completeURL, strings.NewReader(`{"code":"mini:products-v1"}`))
				request.Header.Set("Content-Type", "application/json")
			}
			paymentDefend206Enable(t, f)
			result := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(result, request)
			if p.creates.Load() != 0 {
				t.Fatalf("enabled policy allowed the prepared %s continuation to send: HTTP=%d creates=%d", kind, result.Code, p.creates.Load())
			}
			var attempts int
			if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM provider_attempts WHERE order_id=$1`, o.ID).Scan(&attempts); err != nil || attempts != 0 {
				t.Fatal("blocked continuation created a first attempt", attempts, err)
			}
		})
	}
}
