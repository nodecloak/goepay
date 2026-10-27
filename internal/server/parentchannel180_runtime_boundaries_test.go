package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"

	"goepay/internal/compat"
	"goepay/internal/payment"
)

func TestParentChannel180AllPaymentLifecycleReadersRequireOriginalSource(t *testing.T) {
	for _, operation := range []string{"freeze", "freeze-notify", "freeze-query", "scan"} {
		t.Run(operation, func(t *testing.T) {
			setup := parent180RuntimeReady(t)
			f := setup.f
			if out := parent180WxPay(t, f, setup.id, "parent180-product-source"); out["code"] != float64(1) {
				t.Fatal(out)
			}
			o, e := f.s.Store.Order(context.Background(), 1, "parent180-product-source")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.p.Exec(context.Background(), `ALTER TABLE runtime_parent_payment_sources DISABLE TRIGGER runtime_parent_payment_source_immutable;DELETE FROM runtime_parent_payment_sources;ALTER TABLE runtime_parent_payment_sources ENABLE TRIGGER runtime_parent_payment_source_immutable`); e != nil {
				t.Fatal(e)
			}
			id, _ := strconv.ParseInt(setup.id, 10, 64)
			switch operation {
			case "freeze":
				_, e = f.s.ProviderFlow.SubmitPreauthorization(context.Background(), o.TradeNo, id, "", "")
			case "freeze-notify":
				e = f.s.ProviderFlow.HandlePreauthorizationNotification(context.Background(), id, o.TradeNo, http.Header{}, nil)
			case "freeze-query":
				e = f.s.ProviderFlow.RefreshPreauthorization(context.Background(), o.TradeNo, id)
			case "scan":
				_, e = f.s.ProviderFlow.SubmitScan(context.Background(), o.TradeNo, id, "", "")
			}
			if !errors.Is(e, payment.ErrConflict) {
				t.Fatalf("lifecycle bypassed missing immutable source: %v", e)
			}
			if setup.creates.Load() != 1 || setup.queries.Load() != 0 {
				t.Fatal("lost source reached provider")
			}
		})
	}
}

func parent180Prepared(t *testing.T, setup parent180Runtime, no string) (*Server, payment.Order) {
	t.Helper()
	ctx := context.Background()
	s, e := setup.f.s.withParentRuntime(ctx, 1, no)
	if e != nil {
		t.Fatal(e)
	}
	p := compat.Params{"out_trade_no": no, "type": "wxpay", "name": "synthetic parent180 receiving", "money": "100.00", "channel_id": setup.id, "method": "native", "clientip": "192.0.2.1"}
	id, method, e := s.providerRoutedRequest(ctx, 1, p)
	if e != nil || strconv.FormatInt(id, 10) != setup.id || method != "native" {
		t.Fatal(id, method, e)
	}
	cost, e := s.ProviderFlow.PaymentCostQuote(id, method)
	if e != nil {
		t.Fatal(e)
	}
	order, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: no, Name: p["name"], Type: "wxpay", Amount: 10000, Version: wireVersion(true), PaymentCost: cost})
	if e != nil {
		t.Fatal(e)
	}
	return s, order
}

func TestParentChannel180ActualTLSPreparedCloneCannotBypassDynamicRootWriteGate(t *testing.T) {
	setup := parent180RuntimeReady(t)
	prepared, order := parent180Prepared(t, setup, "parent180-dynamic-gate")
	setup.f.s.ProviderFlow.AllowLive = false
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	_, e := prepared.ProviderFlow.Submit(context.Background(), order.TradeNo, id, "native", "192.0.2.1", "")
	if !errors.Is(e, payment.ErrLiveDisabled) || setup.creates.Load() != 0 {
		t.Fatalf("managed clone bypassed current root gate: creates=%d err=%v", setup.creates.Load(), e)
	}
	var attempts int
	if e = setup.f.p.QueryRow(context.Background(), `SELECT count(*) FROM provider_attempts WHERE order_id=$1`, order.ID).Scan(&attempts); e != nil || attempts != 0 {
		t.Fatal(attempts, e)
	}
}

func TestParentChannel180ActualTLSPreparedCloneCannotBypassCurrentOperatorRevocation(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(strconv.FormatBool(replace), func(t *testing.T) {
			setup := parent180RuntimeReady(t)
			prepared, order := parent180Prepared(t, setup, "parent180-current-operator")
			if replace {
				setup.f.s.MerchantChannels = map[int64][]int64{}
			} else {
				delete(setup.f.s.MerchantChannels, 1)
			}
			id, _ := strconv.ParseInt(setup.id, 10, 64)
			_, e := prepared.ProviderFlow.Submit(context.Background(), order.TradeNo, id, "native", "192.0.2.1", "")
			if e == nil || setup.creates.Load() != 0 {
				t.Fatalf("prepared source bypassed current operator authorization: creates=%d err=%v", setup.creates.Load(), e)
			}
			var attempts int
			if e = setup.f.p.QueryRow(context.Background(), `SELECT count(*) FROM provider_attempts WHERE order_id=$1`, order.ID).Scan(&attempts); e != nil || attempts != 0 {
				t.Fatal(attempts, e)
			}
		})
	}
}

func TestParentChannel180ActualTLSLostImmutableSourceNeverFallsBackToCurrentAccount(t *testing.T) {
	for _, loss := range []string{"source-table", "source-row", "source-and-registration"} {
		t.Run(loss, func(t *testing.T) {
			setup := parent180RuntimeReady(t)
			f := setup.f
			ctx := context.Background()
			if out := parent180WxPay(t, f, setup.id, "parent180-source-loss"); out["code"] != float64(1) {
				t.Fatal(out)
			}
			order, e := f.s.Store.Order(ctx, 1, "parent180-source-loss")
			if e != nil {
				t.Fatal(e)
			}
			switch loss {
			case "source-table":
				_, e = f.p.Exec(ctx, `DROP TABLE runtime_parent_payment_sources`)
			case "source-row":
				_, e = f.p.Exec(ctx, `ALTER TABLE runtime_parent_payment_sources DISABLE TRIGGER runtime_parent_payment_source_immutable;DELETE FROM runtime_parent_payment_sources;ALTER TABLE runtime_parent_payment_sources ENABLE TRIGGER runtime_parent_payment_source_immutable`)
			case "source-and-registration":
				_, e = f.p.Exec(ctx, `DROP TABLE runtime_parent_payment_sources;DELETE FROM schema_migrations WHERE name='000103_runtime_parent_channel_accounts.up.sql'`)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.s.ProviderFlow.Query(ctx, 1, order.TradeNo); e == nil || setup.queries.Load() != 0 {
				t.Fatal("lost original account reached current upstream", setup.queries.Load(), e)
			}
		})
	}
}

func TestParentChannel180ActualTLSCurrentDisableDeniesNewIntentAndPreservesOriginalRead(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := context.Background()
	if out := parent180WxPay(t, f, setup.id, "parent180-before-disable"); out["code"] != float64(1) {
		t.Fatal(out)
	}
	order, e := f.s.Store.Order(ctx, 1, "parent180-before-disable")
	if e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "enabled": false})
	if out := parent180HTTP(f, "PATCH", "/"+setup.id, string(body)); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if out := parent180WxPay(t, f, setup.id, "parent180-after-disable"); out["code"] != float64(-1) || setup.creates.Load() != 1 {
		t.Fatal(out, setup.creates.Load())
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, order.TradeNo); e != nil || setup.queries.Load() != 1 {
		t.Fatal("off gate lost original read", setup.queries.Load(), e)
	}
}

func TestParentChannel180ActualTLSLegalOldFeePolicyCannotRevivePreparedFirstSend(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := context.Background()
	prepared, order := parent180Prepared(t, setup, "parent180-old-fee-policy")
	var old string
	if e := f.p.QueryRow(ctx, `SELECT to_jsonb(p)::text FROM runtime_channel_policies p WHERE channel_id=$1`, setup.id).Scan(&old); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "enabled": false})
	if out := parent180HTTP(f, "PATCH", "/"+setup.id, string(body)); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if _, e := f.p.Exec(ctx, `UPDATE runtime_channel_policies p SET (pay_type,identity_hash,retained_bps,mode,enabled,revision,proof_hash)=(SELECT x.pay_type,x.identity_hash,x.retained_bps,x.mode,x.enabled,x.revision,x.proof_hash FROM jsonb_populate_record(NULL::runtime_channel_policies,$1::jsonb) x) WHERE p.channel_id=$2`, old, setup.id); e != nil {
		t.Fatal(e)
	}
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	if _, e := prepared.ProviderFlow.Submit(ctx, order.TradeNo, id, "native", "192.0.2.1", ""); e == nil || setup.creates.Load() != 0 {
		t.Fatal("legal old fee policy revived external write", setup.creates.Load(), e)
	}
}

func TestParentChannel180ActualTLSAccountRotationAfterSelectionRetainsPreparedAccount(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := context.Background()
	prepared, order := parent180Prepared(t, setup, "parent180-prepared-before-rotation")
	change, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "account": map[string]any{"config": map[string]any{"appkey": map[string]string{"operation": "replace", "value": setup.keyB}}, "certificates": map[string]any{"sslcert_path": map[string]string{"operation": "replace", "value": setup.certB}, "sslkey_path": map[string]string{"operation": "replace", "value": setup.pemB}}}})
	if out := parent180HTTP(f, "PATCH", "/"+setup.id, string(change)); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	if _, e := prepared.ProviderFlow.Submit(ctx, order.TradeNo, id, "native", "192.0.2.1", ""); e != nil || setup.creates.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("prepared original source was replaced or rejected", setup.creates.Load(), setup.bad.Load(), e)
	}
}

func TestParentChannel180ActualTLSRegisteredParentCapturesOriginalBeforeConfigurationEdit(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := context.Background()
	if out := parent180WxPay(t, f, "8", "parent180-original-bootstrap"); out["code"] != float64(1) {
		t.Fatal(out)
	}
	var heads, accounts, sources, feePolicies int
	if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM runtime_parent_channels WHERE id=8 AND legacy_origin),(SELECT count(*) FROM runtime_parent_channel_accounts WHERE channel_id=8 AND version=1),(SELECT count(*) FROM runtime_parent_payment_sources WHERE channel_id=8),(SELECT count(*) FROM runtime_channel_policies WHERE channel_id=8)`).Scan(&heads, &accounts, &sources, &feePolicies); e != nil || heads != 1 || accounts != 1 || sources != 1 || feePolicies != 0 {
		t.Fatal("registered original execution was not atomically captured or legacy fee was rewritten", heads, accounts, sources, feePolicies, e)
	}
	listed := parent180HTTP(f, "GET", "", "")
	if listed.Code != 200 {
		t.Fatal("captured legacy parent became unreadable", listed.Code, listed.Body.String())
	}
	var catalog struct {
		Data struct{ Items []map[string]any }
	}
	if e := json.Unmarshal(listed.Body.Bytes(), &catalog); e != nil {
		t.Fatal(e)
	}
	var original map[string]any
	for _, row := range catalog.Data.Items {
		if row["id"] == "8" {
			original = row
		}
	}
	if original == nil || original["rate"] != nil {
		t.Fatal("legacy unknown rate was fabricated", original)
	}
	change, _ := json.Marshal(map[string]any{"expected_fingerprint": original["fingerprint"], "account": map[string]any{"config": map[string]any{"appkey": map[string]string{"operation": "replace", "value": setup.keyB}}, "certificates": map[string]any{"sslcert_path": map[string]string{"operation": "replace", "value": setup.certB}, "sslkey_path": map[string]string{"operation": "replace", "value": setup.pemB}}}})
	if out := parent180HTTP(f, "PATCH", "/8", string(change)); out.Code != 200 {
		t.Fatal("registered credential edit unavailable", out.Code, out.Body.String())
	}
	setup.expectedCreate.Store("B")
	if out := parent180WxPay(t, f, "8", "parent180-after-bootstrap-rotation"); out["code"] != float64(1) {
		t.Fatal(out)
	}
	order, e := f.s.Store.Order(ctx, 1, "parent180-original-bootstrap")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, order.TradeNo); e != nil || setup.bad.Load() != 0 {
		t.Fatal("original registered account did not survive config edit", setup.bad.Load(), e)
	}
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM runtime_channel_policies WHERE channel_id=8`).Scan(&feePolicies); e != nil || feePolicies != 0 {
		t.Fatal("credential edit changed original fee policy", feePolicies, e)
	}
}

func TestParentChannel180ActualTLSPreVersionHistoryCannotBorrowNewManagedAccount(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := context.Background()
	// This synthetic historical attempt deliberately has the pre180 account
	// proof and no parent execution source. A later snapshot cannot prove its key.
	old, e := f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "parent180-preversion", Name: "historical synthetic", Type: "wxpay", Amount: 10000})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Submit(ctx, old.TradeNo, 8, "native", "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	if out := parent180WxPay(t, f, "8", "parent180-captured-new-intent"); out["code"] != float64(1) {
		t.Fatal(out)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, old.TradeNo); e == nil || setup.queries.Load() != 0 {
		t.Fatal("new managed snapshot was invented as historical secret version", setup.queries.Load(), e)
	}
}
