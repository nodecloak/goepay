package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestGlobalAccess184PlatformGateIntersectsOriginalChildAndRestoresOriginalRefund(t *testing.T) {
	f, factory, row := subchannel168BProductLocal(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"daytop": "100.00"})
	if out := f.pay(t, "platform184-child", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, err := f.s.Store.Order(ctx, 1, "platform184-child")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	request := func(method, body string) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, "/console/v1/admin/settings/merchant-access", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return channel168Data(t, w)
	}
	state := request("GET", "")
	set := func(value string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"close_keylogin": state["close_keylogin"].(string), "user_refund": value, "expected_fingerprint": state["fingerprint"].(string)})
		state = request("PATCH", string(body))
	}
	capability := func() bool {
		t.Helper()
		items := []map[string]any{{"id": fmt.Sprint(o.ID)}}
		if err := f.s.merchantOrderActions(ctx, identity.Session{User: identity.User{ID: 1, Role: "merchant"}}, items); err != nil {
			t.Fatal(err)
		}
		return items[0]["merchant_actions"].(map[string]bool)["refund"]
	}
	if !capability() {
		t.Fatal("original child capability was replaced by parent capability")
	}
	set("0")
	if capability() {
		t.Fatal("platform closed but original child refund remained advertised")
	}
	if _, err = f.s.ProviderFlow.RefundFromMerchant(ctx, 1, o.TradeNo, "closed-child184", 100); !errors.Is(err, payment.ErrMerchantAPIDisabled) {
		t.Fatal(err)
	}
	var count int
	if err = f.p.QueryRow(ctx, `SELECT count(*) FROM refunds WHERE order_id=$1`, o.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("closed child reserved money", count, err)
	}
	subchannel168BRotate(t, f, row)
	channel168CSetLimits(t, f, map[string]string{"paymin": "2.00"})
	set("1")
	if !capability() {
		t.Fatal("current child or current parent limits erased original refund capability")
	}
	r, err := f.s.ProviderFlow.RefundFromMerchant(ctx, 1, o.TradeNo, "original-child184", 100)
	if err != nil || r.Status != "succeeded" {
		t.Fatal(r.Status, err)
	}
	if factory.providers["products-v1"].refunds.Load() != 1 || factory.providers["products-v2"].refunds.Load() != 0 {
		t.Fatal("reopened refund used current child account")
	}
	if capability() {
		t.Fatal("fully refunded child still advertised a new refund")
	}
	if _, err = f.s.Store.CheckLedger(ctx); err != nil {
		t.Fatal(err)
	}
}
