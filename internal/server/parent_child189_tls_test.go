package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Only this fixture replaces official transports with the existing local mTLS
// peer. All constructors, closed schemas, snapshots, HMACs and wire signatures
// are the real implementations; production has no configurable peer URL.
type child189TLSFactory struct {
	adapter.SubchannelFactory
	local *parent180TLSFactory
}

func (f child189TLSFactory) Restore(raw []byte) (adapter.SubchannelExecution, error) {
	execution, err := f.SubchannelFactory.Restore(raw)
	if err != nil {
		return execution, err
	}
	var snapshot struct{ Channel channelconfig.Channel }
	if json.Unmarshal(raw, &snapshot) != nil {
		return execution, adapter.ErrInvalid
	}
	_, parentRaw, err := f.local.ParentChannelFactory.BuildSnapshot(snapshot.Channel.ID, snapshot.Channel)
	if err != nil {
		return execution, err
	}
	local, err := f.local.local(parentRaw)
	if err != nil {
		return execution, err
	}
	execution.Payment = local.Payment
	return execution, nil
}
func (f child189TLSFactory) Build(values map[string]string) (adapter.SubchannelExecution, error) {
	raw, err := f.SubchannelFactory.Snapshot(values)
	if err != nil {
		return adapter.SubchannelExecution{}, err
	}
	return f.Restore(raw)
}
func (f child189TLSFactory) DeriveParentSubchannel(id int64, raw []byte) (adapter.SubchannelFactory, error) {
	base, ok := f.SubchannelFactory.(adapter.SubchannelAliasFactory)
	if !ok {
		return nil, adapter.ErrInvalid
	}
	derived, err := base.DeriveParentSubchannel(id, raw)
	if err != nil {
		return nil, err
	}
	return child189TLSFactory{derived, f.local}, nil
}
func (f child189TLSFactory) ParentSubchannelOrigin() (int64, []byte) {
	return f.SubchannelFactory.(adapter.SubchannelAliasFactory).ParentSubchannelOrigin()
}

func child189Setup(t *testing.T) (parent180Runtime, map[string]string, map[string]any) {
	t.Helper()
	setup := parent180RuntimeReady(t)
	local := setup.f.s.ProviderFlow.ParentFactories[8].(*parent180TLSFactory)
	origin := local.Bootstrap()
	factories, err := channelconfig.BuildPaymentSubchannelFactories(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{origin}, PaymentSubchannelTemplates: []channelconfig.PaymentSubchannelTemplate{{ChannelID: 8, Revision: 1, ConfigSlots: map[string]string{"appid": "app", "appmchid": "merchant", "appkey": "key"}, CertificateSlots: map[string]string{"sslcert_path": "certificate", "sslkey_path": "private_key"}}}}, channelconfig.BuildOptions{AllowLive: true})
	if err != nil {
		t.Fatal(err)
	}
	setup.f.s.ProviderFlow.SubchannelFactories = map[int64]adapter.SubchannelFactory{8: child189TLSFactory{factories[8], local}}
	if _, err = setup.f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=body||'{"info":{"wxpay":{"channel":"-2","type":"channel"}}}'::jsonb WHERE resource='groups' AND id=0;INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"app": "child189-A", "merchant": "child189-merchant-A", "key": "abcdef0123456789abcdef0123456789", "certificate": origin.Certificates["sslcert_path"], "private_key": origin.Certificates["sslkey_path"]}
	raw, _ := json.Marshal(map[string]any{"merchant_id": "1", "parent_id": setup.id, "name": "original alias child189", "enabled": true, "weight": 1, "min_amount": "1", "max_amount": "100000", "values": values})
	w := setup.f.childHTTP("POST", "", string(raw))
	if w.Code != 200 {
		t.Fatal("alias child creation failed", w.Code)
	}
	var saved struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &saved) != nil || saved.Data["id"] == nil {
		t.Fatal("child response invalid")
	}
	return setup, values, saved.Data
}

func child189Pay(t *testing.T, f channel168Fixture, child, no string) {
	t.Helper()
	p := compat.Params{"pid": "1", "type": "wxpay", "out_trade_no": no, "name": "original child189", "money": "100.00", "subchannel_id": child, "method": "native", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
	p["sign"] = compat.SignMD5(p, "test-secret")
	encoded := url.Values{}
	for key, value := range p {
		encoded.Set(key, value)
	}
	form := strings.NewReader(encoded.Encode())
	r := httptest.NewRequest("POST", "/mapi.php", form)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var out struct {
		Code int
		Msg  string
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Code != 1 {
		t.Fatal("actual alias child payment rejected", w.Code, out.Msg)
	}
}

func TestParentChild189ActualAliasTLSKeepsOriginalChildAcrossIndependentParentAndChildRotation(t *testing.T) {
	setup, values, child := child189Setup(t)
	f, ctx := setup.f, context.Background()
	id := child["id"].(string)
	child189Pay(t, f, id, "child189-original")
	original, err := f.s.Store.Order(ctx, 1, "child189-original")
	if err != nil || original.MerchantAmount != 9950 {
		t.Fatal("original child fee changed", err)
	}
	change, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "account": map[string]any{"config": map[string]any{"appkey": map[string]string{"operation": "replace", "value": setup.keyB}}, "certificates": map[string]any{"sslcert_path": map[string]string{"operation": "replace", "value": setup.certB}, "sslkey_path": map[string]string{"operation": "replace", "value": setup.pemB}}}})
	if w := parent180HTTP(f, "PATCH", "/"+setup.id, string(change)); w.Code != 200 {
		t.Fatal("parent rotate rejected", w.Code)
	}
	// The new parent uses B, while this original child still signs with A.
	child189Pay(t, f, id, "child189-after-parent-only")
	values["app"], values["merchant"], values["key"] = "child189-B", "child189-merchant-B", setup.keyB
	values["certificate"], values["private_key"] = setup.certB, setup.pemB
	fields := map[string]any{}
	for key, value := range values {
		fields[key] = map[string]string{"operation": "replace", "value": value}
	}
	patch, _ := json.Marshal(map[string]any{"expected_fingerprint": child["fingerprint"], "values": fields})
	if w := f.childHTTP("PATCH", "/"+id, string(patch)); w.Code != 200 {
		t.Fatal("child rotation rejected", w.Code)
	}
	setup.expectedCreate.Store("B")
	child189Pay(t, f, id, "child189-after-child-rotation")
	if _, err = f.s.ProviderFlow.Query(ctx, 1, original.TradeNo); err != nil {
		t.Fatal("old child query changed SDK", err)
	}
	if _, err = f.s.ProviderFlow.Refund(ctx, 1, original.TradeNo, "child189-original-refund", 1000); err != nil {
		t.Fatal("old child refund changed SDK", err)
	}
	if setup.creates.Load() != 3 || setup.queries.Load() != 1 || setup.refunds.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("unexpected peer counts", setup.creates.Load(), setup.queries.Load(), setup.refunds.Load(), setup.bad.Load())
	}
	parentID, _ := strconv.ParseInt(setup.id, 10, 64)
	flow, _, err := f.s.ProviderFlow.ForPaymentIntent(ctx, 1, original.OutTradeNo, parentID)
	if err != nil || flow.Providers[parentID].(interface{ ApplicationID() string }).ApplicationID() != "child189-A" {
		t.Fatal("original physical parent used latest account", err)
	}
	var count int
	if err = f.p.QueryRow(ctx, `SELECT count(*) FROM payment_subchannel_routes WHERE parent_id=$1`, parentID).Scan(&count); err != nil || count != 3 {
		t.Fatal(fmt.Sprint(count), err)
	}
	// Removing mutable management state must not substitute today's parent
	// credentials for the two independently sealed original execution sources.
	tx, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `ALTER TABLE runtime_parent_channels DISABLE TRIGGER ALL`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM runtime_parent_channels WHERE id=$1`, parentID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `ALTER TABLE runtime_parent_channels ENABLE TRIGGER ALL`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProviderFlow.Query(ctx, 1, original.TradeNo); err != nil || setup.queries.Load() != 2 || setup.bad.Load() != 0 {
		t.Fatal("lost parent head changed original child recovery", err)
	}
}

func child189Prepared(t *testing.T, setup parent180Runtime, child, no string) (*Server, payment.Order, int64) {
	t.Helper()
	ctx := t.Context()
	view, err := setup.f.s.withParentRuntime(ctx, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	p := compat.Params{"out_trade_no": no, "type": "wxpay", "name": "prepared original child189", "money": "100.00", "subchannel_id": child, "method": "native", "clientip": "192.0.2.189"}
	channel, method, err := view.providerRoutedRequest(ctx, 1, p)
	if err != nil {
		t.Fatal("prepare alias route", err)
	}
	view, err = view.paymentIntentServer(ctx, 1, no, channel)
	if err != nil {
		t.Fatal("prepare original execution", err)
	}
	cost, err := view.ProviderFlow.PaymentCostQuote(channel, method)
	if err != nil {
		t.Fatal(err)
	}
	order, err := view.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: no, Type: "wxpay", Name: p["name"], Amount: 10000, PaymentCost: cost, Version: "v2"})
	if err != nil {
		t.Fatal(err)
	}
	return view, order, channel
}

func TestParentChild189PreparedAliasRequiresCurrentAuthorityWithoutChangingHistoricalQueries(t *testing.T) {
	for _, revoke := range []string{"root", "sandbox", "operator-map", "parent-off", "grant"} {
		t.Run(revoke, func(t *testing.T) {
			setup, _, child := child189Setup(t)
			id := child["id"].(string)
			child189Pay(t, setup.f, id, "child189-read-original")
			old, err := setup.f.s.Store.Order(t.Context(), 1, "child189-read-original")
			if err != nil {
				t.Fatal(err)
			}
			view, order, channel := child189Prepared(t, setup, id, "child189-prepared-revoked")
			switch revoke {
			case "root":
				setup.f.s.ProviderFlow.AllowLive = false
			case "sandbox":
				setup.f.s.Sandbox = true
			case "operator-map":
				setup.f.s.MerchantChannels = map[int64][]int64{}
			case "parent-off":
				body, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "enabled": false})
				if w := parent180HTTP(setup.f, "PATCH", "/"+setup.id, string(body)); w.Code != 200 {
					t.Fatal("off failed", w.Code)
				}
			case "grant":
				tx, err := setup.f.p.Begin(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(t.Context())
				if _, err = tx.Exec(t.Context(), `ALTER TABLE runtime_parent_channel_grants DISABLE TRIGGER runtime_parent_grant_immutable`); err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec(t.Context(), `DELETE FROM runtime_parent_channel_grants WHERE channel_id=$1`, channel); err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec(t.Context(), `ALTER TABLE runtime_parent_channel_grants ENABLE TRIGGER runtime_parent_grant_immutable`); err != nil {
					t.Fatal(err)
				}
				if err = tx.Commit(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = view.ProviderFlow.Submit(t.Context(), order.TradeNo, channel, "native", "192.0.2.189", ""); err == nil {
				t.Fatal("revoked prepared send succeeded")
			}
			var attempts int
			if err = setup.f.p.QueryRow(t.Context(), `SELECT count(*) FROM provider_attempts WHERE order_id=$1`, order.ID).Scan(&attempts); err != nil || attempts != 0 || setup.creates.Load() != 1 {
				t.Fatal("revoked first send consumed intent", attempts, err, setup.creates.Load())
			}
			if _, err = setup.f.s.ProviderFlow.Query(t.Context(), 1, old.TradeNo); err != nil || setup.queries.Load() != 1 || setup.bad.Load() != 0 {
				t.Fatal("current revoke changed original query account", err)
			}
		})
	}
}

func TestParentChild189LostRequiredParentSourceCannotBorrowCompleteChild(t *testing.T) {
	setup, _, child := child189Setup(t)
	child189Pay(t, setup.f, child["id"].(string), "child189-lost-parent-source")
	order, err := setup.f.s.Store.Order(t.Context(), 1, "child189-lost-parent-source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = setup.f.p.Exec(t.Context(), `ALTER TABLE runtime_parent_payment_sources DISABLE TRIGGER runtime_parent_payment_source_immutable;DELETE FROM runtime_parent_payment_sources;ALTER TABLE runtime_parent_payment_sources ENABLE TRIGGER runtime_parent_payment_source_immutable`); err != nil {
		t.Fatal(err)
	}
	if _, err = setup.f.s.ProviderFlow.Query(t.Context(), 1, order.TradeNo); err == nil || setup.queries.Load() != 0 {
		t.Fatal("new parent source loss used complete child as fallback", err)
	}
}

func TestParentChild189ActualHTTPCallbackAuthenticatesOriginalChildAfterParentRotationAndDisable(t *testing.T) {
	setup, _, child := child189Setup(t)
	child189Pay(t, setup.f, child["id"].(string), "child189-http-original")
	order, err := setup.f.s.Store.Order(t.Context(), 1, "child189-http-original")
	if err != nil { t.Fatal(err) }
	parent180RotateOutgoing(t, setup)
	read := parent180HTTP(setup.f, "GET", "/"+setup.id, "")
	var state struct{ Data map[string]any }
	if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &state) != nil { t.Fatal("current parent unavailable") }
	body, _ := json.Marshal(map[string]any{"expected_fingerprint":state.Data["fingerprint"], "enabled":false})
	if w := parent180HTTP(setup.f, "PATCH", "/"+setup.id, string(body)); w.Code != 200 { t.Fatal("disable failed", w.Code) }
	setup.f.s.ProviderFlow.AllowLive = false
	callback := map[string]string{"return_code":"SUCCESS", "result_code":"SUCCESS", "appid":"child189-A", "mch_id":"child189-merchant-A", "out_trade_no":order.TradeNo, "transaction_id":"WX180_"+order.TradeNo, "total_fee":"10000", "trade_type":"NATIVE", "openid":"synthetic189-buyer"}
	send := func(key string) int {
		r := httptest.NewRequest("POST", "/callbacks/providers/"+setup.id+"/"+order.TradeNo, bytes.NewReader(parent180SignedXML(callback, key)))
		r.Header.Set("Content-Type", "application/xml")
		w := httptest.NewRecorder(); setup.f.s.Handler().ServeHTTP(w, r); return w.Code
	}
	if status := send(setup.keyB); status != 400 { t.Fatal("latest parent key authenticated original child", status) }
	if status := send("abcdef0123456789abcdef0123456789"); status != 200 { t.Fatal("original child callback could not recover", status) }
	paid, err := setup.f.s.Store.Order(t.Context(), 1, order.OutTradeNo)
	if err != nil || paid.Status != "paid" || setup.creates.Load() != 1 || setup.bad.Load() != 0 { t.Fatal("callback did not settle original child", err, paid.Status) }
}
