package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubchannel168BMissingCurrentChildSchemaCannotBecomeHistoricalParentExecution(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	child168BCreate(t, f, "app168-seven", true)
	if out := f.pay(t, "same-account-source168b", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	if _, e := f.p.Exec(context.Background(), `DROP TABLE payment_subchannel_routes CASCADE`); e != nil {
		t.Fatal(e)
	}
	_, _, e := f.s.ProviderFlow.ForPaymentIntent(context.Background(), 1, "same-account-source168b", 7)
	if !errors.Is(e, payment.ErrConflict) {
		t.Fatal("missing current child schema downgraded to parent", e)
	}
}
func TestSubchannel168BHeadMACAndEncryptedAccountCannotBeSpliced(t *testing.T) {
	for _, kind := range []string{"head", "account"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := subchannel168BLocal(t)
			child168BCreate(t, f, "splice-child", true)
			ctx := context.Background()
			query := `UPDATE payment_subchannels SET proof_hash=decode(repeat('00',32),'hex')`
			if kind == "account" {
				query = `ALTER TABLE payment_subchannel_accounts DISABLE TRIGGER payment_subchannel_account_immutable; UPDATE payment_subchannel_accounts SET ciphertext=decode(repeat('00',64),'hex'); ALTER TABLE payment_subchannel_accounts ENABLE TRIGGER payment_subchannel_account_immutable`
			}
			if _, e := f.p.Exec(ctx, query); e != nil {
				t.Fatal(e)
			}
			if out := f.pay(t, "splice-source-"+kind, "1.00", 7); out["code"] == float64(1) {
				t.Fatal("corrupt authenticated child state admitted payment")
			}
			w := f.childHTTP("GET", "", "")
			if w.Code != 409 || strings.Contains(w.Body.String(), "synthetic-only-secret") {
				t.Fatal("corrupt source was published", w.Code)
			}
		})
	}
}
func TestSubchannel168BAllRouteSourcesMissingCannotBorrowIdenticalParentAccountOrCapabilities(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	parent := &subchannel168BProducts{channel168Provider: &channel168Provider{app: "products-v1"}}
	f.s.ProviderFlow.Providers[7] = parent
	for _, no := range []string{"all-loss-pending168b", "all-loss-paid168b"} {
		if out := subchannel168BProductPay(t, f, no, "native", nil); out["code"] != float64(1) {
			t.Fatal(out)
		}
	}
	ctx := context.Background()
	pending, _ := f.s.Store.Order(ctx, 1, "all-loss-pending168b")
	paid, _ := f.s.Store.Order(ctx, 1, "all-loss-paid168b")
	if _, e := f.s.ProviderFlow.Query(ctx, 1, paid.TradeNo); e != nil {
		t.Fatal(e)
	}
	// Synthetic storage-loss fixture only: keep independent order birth fields and
	// referral MACs, but remove every mutable route/quote/terms source row.
	for _, table := range []string{"payment_subchannel_last_used", "payment_subchannel_routes", "order_route_fee_terms", "provider_route_fee_quotes", "provider_route_assignments"} {
		if _, e := f.p.Exec(ctx, `ALTER TABLE `+table+` DISABLE TRIGGER ALL; DELETE FROM `+table+`; ALTER TABLE `+table+` ENABLE TRIGGER ALL`); e != nil {
			t.Fatal(table, e)
		}
	}
	if _, _, e := f.s.ProviderFlow.ForPaymentIntent(ctx, 1, pending.OutTradeNo, 7); e == nil {
		t.Error("all child sources missing silently became parent execution")
	}
	if v, e := f.s.Store.OriginalSubchannelContext(ctx, 1, pending.OutTradeNo, 7); e == nil {
		t.Error("source loss was shown as ordinary historical order", v)
	}
	caps, e := f.s.ProviderFlow.OriginalOrderCapabilities(ctx, 1, []int64{pending.ID, paid.ID})
	if e == nil && (caps[pending.ID].Query || caps[paid.ID].Refund) {
		t.Error("source loss borrowed parent list capability")
	}
	token, csrf := child168BMerchantSession(t, f, 1)
	for _, role := range []string{"admin", "merchant"} {
		var w *httptest.ResponseRecorder
		if role == "merchant" {
			w = child168BMerchantRequest(f, token, csrf, "GET", "orders/"+pending.TradeNo, "")
		} else {
			r := httptest.NewRequest("GET", "/console/v1/admin/orders/"+pending.TradeNo, nil)
			r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
			w = httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, r)
		}
		if w.Code != 200 {
			t.Fatal("source-loss detail did not retain explicit unavailable context", role, w.Code, w.Body.String())
		}
		var payload struct{ Data map[string]any }
		if e = json.Unmarshal(w.Body.Bytes(), &payload); e != nil {
			t.Fatal(e)
		}
		provider := payload.Data["provider"].(map[string]any)
		if provider["can_close"] != false || payload.Data["payment_subchannel_unconfirmed"] != true {
			t.Error("source-loss detail borrowed close capability", role, provider, payload.Data["payment_subchannel_unconfirmed"])
		}
	}
	f.s.ProviderFlow.Query(ctx, 1, pending.TradeNo)
	if parent.queries.Load() != 0 || parent.refunds.Load() != 0 {
		t.Error("parent received I/O before missing immutable source was rejected", parent.queries.Load(), parent.refunds.Load())
	}
	if factory.providers["products-v1"].creates.Load() != 2 {
		t.Fatal("source loss caused new child payment")
	}
}
func TestSubchannel168BMissingSchemaAndMigrationMarkerNeverBorrowParentListCapabilities(t *testing.T) {
	for _, lostQuote := range []bool{false, true} {
		t.Run(fmt.Sprint(lostQuote), func(t *testing.T) {
			f, _, _ := subchannel168BProductLocal(t)
			parent := &subchannel168BProducts{channel168Provider: &channel168Provider{app: "products-v1"}}
			f.s.ProviderFlow.Providers[7] = parent
			if out := subchannel168BProductPay(t, f, "missing-schema-cap168b", "native", nil); out["code"] != float64(1) {
				t.Fatal(out)
			}
			ctx := context.Background()
			o, e := f.s.Store.Order(ctx, 1, "missing-schema-cap168b")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.p.Exec(ctx, `DROP TABLE payment_subchannel_routes CASCADE; DELETE FROM schema_migrations WHERE name='000098_merchant_payment_subchannels.up.sql'`); e != nil {
				t.Fatal(e)
			}
			if lostQuote {
				for _, table := range []string{"order_route_fee_terms", "provider_route_fee_quotes", "provider_route_assignments"} {
					if _, e = f.p.Exec(ctx, `ALTER TABLE `+table+` DISABLE TRIGGER ALL; DELETE FROM `+table+`; ALTER TABLE `+table+` ENABLE TRIGGER ALL`); e != nil {
						t.Fatal(e)
					}
				}
			}
			caps, e := f.s.ProviderFlow.OriginalOrderCapabilities(ctx, 1, []int64{o.ID})
			if e == nil && caps[o.ID].Query {
				t.Fatal("missing schema+registration used legacy parent capability", lostQuote)
			}
		})
	}
}
