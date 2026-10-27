package contract_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func merchantRequest179(f signatureFixture163, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/merchant/"+path, strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func TestGlobalAccess179MerchantRefundEntryAndCapabilitiesFollowPlatform(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			f, p, o := merchant174Runtime(t)
			if !enabled {
				globalSetting179(t, f, "user_refund", "0")
			}
			w := merchantRequest179(f, "GET", "orders/"+o.TradeNo, "")
			if w.Code != 200 {
				t.Fatal("original owned detail positive", w.Code)
			}
			data := credentialData162(t, w)
			caps, ok := data["merchant_actions"].(map[string]any)
			if !ok || caps["refund"] != enabled {
				t.Errorf("merchant refund capability ignored actual platform state: enabled=%v caps=%v", enabled, caps)
			}
			w = merchantRequest179(f, "POST", "orders/"+o.TradeNo+"/refund", `{"amount":"1.00","external_no":"self-access179"}`)
			if enabled {
				if w.Code != 200 && w.Code != 202 || p.refundWrites.Load() != 1 {
					t.Fatalf("original self refund positive unavailable: HTTP=%d sends=%d", w.Code, p.refundWrites.Load())
				}
			} else if w.Code != 403 || p.refundWrites.Load() != 0 {
				t.Fatalf("platform close did not stop owned self refund: HTTP=%d sends=%d", w.Code, p.refundWrites.Load())
			}
		})
	}
}
