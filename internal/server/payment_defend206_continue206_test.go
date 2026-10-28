package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"goepay/internal/compat"
	"goepay/internal/paymentdefend206"
)

// defend206Continuation prepares an OAuth (web) or mini order under the
// current policy and returns the final continuation request.
func defend206Continuation(t *testing.T, kind, no string, configure func(channel168Fixture)) (channel168Fixture, *subchannel168BProducts, *http.Request, int64) {
	t.Helper()
	f, factory, _ := subchannel168BProductLocal(t)
	var err error
	f.s.PaymentDefend206, err = paymentdefend206.NewStore(f.p, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if configure != nil {
		configure(f)
	}
	method, extra := "jsapi", compat.Params{"oauth": "1"}
	if kind == "mini" {
		method, extra = "applet", nil
	}
	if out := subchannel168BProductPay(t, f, no, method, extra); out["code"] != float64(1) {
		t.Fatal("policy did not admit the original intent", out)
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
	if kind == "web" {
		u, err := url.Parse(checkout.Header().Get("Location"))
		if checkout.Code != http.StatusFound || err != nil {
			t.Fatal("original OAuth entry unavailable", checkout.Code)
		}
		callback := u.Query().Get("redirect_uri") + "?" + url.Values{"state": {u.Query().Get("state")}, "auth_code": {"code:products-v1"}, "app_id": {"products-v1"}}.Encode()
		r := httptest.NewRequest(http.MethodGet, callback, nil)
		for _, cookie := range checkout.Result().Cookies() {
			r.AddCookie(cookie)
		}
		return f, p, r, o.ID
	}
	if checkout.Code != http.StatusOK || p.completeURL == "" {
		t.Fatal("original mini entry unavailable", checkout.Code)
	}
	r := httptest.NewRequest(http.MethodPost, p.completeURL, strings.NewReader(`{"code":"mini:products-v1"}`))
	r.Header.Set("Content-Type", "application/json")
	return f, p, r, o.ID
}

// Slice 6: an order admitted at creation resumes its OAuth/mini continuation
// once under the same policy revision, without a second verification.
func TestPaymentDefend206ContinuationResumesAdmittedOrderOnce(t *testing.T) {
	for _, kind := range []string{"web", "mini"} {
		t.Run(kind, func(t *testing.T) {
			// pay_verify=2 lists another merchant: merchant 1 is admitted as not
			// triggered, under a configured (non-off) policy.
			f, p, r, _ := defend206Continuation(t, kind, "defend206-resume-"+kind, func(f channel168Fixture) {
				defendList206Configure(t, f, "2", "3", "9")
			})
			w := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, r)
			if p.creates.Load() != 1 {
				t.Fatalf("admitted %s continuation did not send exactly once: HTTP=%d creates=%d body=%s", kind, w.Code, p.creates.Load(), w.Body.String())
			}
		})
	}
}

func TestPaymentDefend206ContinuationTamperedEvidenceFailsClosed(t *testing.T) {
	f, p, r, orderID := defend206Continuation(t, "web", "defend206-tamper-web", func(f channel168Fixture) {
		defendList206Configure(t, f, "2", "3", "9")
	})
	if _, err := f.p.Exec(t.Context(), `ALTER TABLE payment_defend206_admissions DISABLE TRIGGER payment_defend206_admissions_original;
 UPDATE payment_defend206_admissions SET kind='0' WHERE out_trade_no='defend206-tamper-web';
 ALTER TABLE payment_defend206_admissions ENABLE TRIGGER payment_defend206_admissions_original`); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var attempts int
	if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM provider_attempts WHERE order_id=$1`, orderID).Scan(&attempts); err != nil || attempts != 0 || p.creates.Load() != 0 {
		t.Fatal("tampered admission evidence resumed a continuation", attempts, p.creates.Load(), err)
	}
}

func TestPaymentDefend206AdmissionEvidenceIsImmutable(t *testing.T) {
	f, _, _, _ := defend206Continuation(t, "web", "defend206-immutable", nil)
	if _, err := f.p.Exec(t.Context(), `DELETE FROM payment_defend206_admissions`); err == nil {
		t.Fatal("admission evidence could be deleted")
	}
	var n int
	if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM payment_defend206_admissions WHERE out_trade_no='defend206-immutable' AND kind='none'`).Scan(&n); err != nil || n != 1 {
		t.Fatal("not-triggered admission was not recorded", n, err)
	}
}
