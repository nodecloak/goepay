package server

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

func TestDefaultPaymentParent236BootstrapCollectsWithoutGrantingOutgoingAuthority(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	delete(f.s.MerchantChannels, 1)
	f.s.DefaultPaymentChannels = []int64{8}
	if out := parent180WxPay(t, f, "8", "default-parent236-public"); out["code"] != float64(1) {
		t.Fatal("configured public origin did not reach the real parent SDK", out)
	}
	var channel, version int64
	if err := f.p.QueryRow(t.Context(), `SELECT channel_id,account_version FROM runtime_parent_payment_sources WHERE merchant_id=1 AND out_trade_no='default-parent236-public'`).Scan(&channel, &version); err != nil || channel != 8 || version != 1 {
		t.Fatal("default collection did not seal its actual parent source", channel, version, err)
	}
	if setup.creates.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("default collection did not use valid mutual TLS and MAC", setup.creates.Load(), setup.bad.Load())
	}
	tx, err := f.p.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	outgoing, err := f.s.transferRuntimeSnapshot228(t.Context(), tx, 1)
	if err != nil || len(outgoing.MerchantChannels[1]) != 0 {
		t.Fatal("public collection source became outgoing authority", err)
	}
	if _, present := f.s.MerchantChannels[1]; present || len(f.s.ProviderFlow.MerchantChannels[1]) != 0 || setup.transfers.Load() != 0 {
		t.Fatal("collection request mutated the generic root or payout map")
	}
}

func defaultParent236OtherMerchantPay(t *testing.T, f channel168Fixture, channel, no string) map[string]any {
	t.Helper()
	p := compat.Params{"pid": "2", "out_trade_no": no, "type": "wxpay", "name": "Synthetic default parent236", "money": "100.00", "channel_id": channel, "method": "native", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
	p["sign"] = compat.SignMD5(p, "synthetic-default236-owner-two")
	v := url.Values{}
	for key, value := range p {
		v.Set(key, value)
	}
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal("invalid synthetic merchant response", w.Code, err)
	}
	return out
}

func TestDefaultPaymentParent236PrivateAliasStillRequiresItsOriginalMerchantGrant(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	if err := f.s.Store.CreateMerchant(t.Context(), payment.Merchant{ID: 2, Name: "Synthetic default236 second merchant", APIKey: "synthetic-default236-owner-two"}); err != nil {
		t.Fatal(err)
	}
	delete(f.s.MerchantChannels, 1)
	f.s.DefaultPaymentChannels = []int64{8}
	var grants int
	if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM runtime_parent_channel_grants WHERE channel_id=$1 AND merchant_id=1`, setup.id).Scan(&grants); err != nil || grants != 1 {
		t.Fatal("fixture did not create the real administrator-authorized alias grant", grants, err)
	}
	if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_set(body,'{info}',jsonb_build_object('wxpay',jsonb_build_object('channel',$1::text,'type','channel'))) WHERE resource='groups' AND id=0`, setup.id); err != nil {
		t.Fatal(err)
	}
	if out := defaultParent236OtherMerchantPay(t, f, setup.id, "default-parent236-private-denied"); out["code"] == float64(1) {
		t.Fatal("group route granted another merchant a private parent alias", out)
	}
	if setup.creates.Load() != 0 {
		t.Fatal("ungranted alias reached the upstream SDK")
	}
	if out := parent180WxPay(t, f, setup.id, "default-parent236-private-granted"); out["code"] != float64(1) {
		t.Fatal("existing signed alias grant was lost for the default merchant", out)
	}
	if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM runtime_parent_channel_grants WHERE channel_id=$1 AND merchant_id=2`, setup.id).Scan(&grants); err != nil || grants != 0 || setup.creates.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("default lookup fabricated a private grant or used the wrong account", grants, setup.creates.Load(), setup.bad.Load(), err)
	}
}

func TestDefaultPaymentParent236SealedReplayAndHistorySurviveClosedCurrentRouting(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	delete(f.s.MerchantChannels, 1)
	f.s.DefaultPaymentChannels = []int64{8}
	roll, _ := json.Marshal(map[string]any{"name": "Synthetic default236 original roll", "merchant_id": "0", "type": "wxpay", "strategy": "first", "channel_ids": setup.id + ":1", "enabled": true, "default_route": false})
	var rollID int64
	if err := f.p.QueryRow(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('pay-rolls',$1::jsonb) RETURNING id`, string(roll)).Scan(&rollID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_set(body,'{info}',jsonb_build_object('wxpay',jsonb_build_object('channel',$1::text,'type','roll'))) WHERE resource='groups' AND id=0`, strconv.FormatInt(rollID, 10)); err != nil {
		t.Fatal(err)
	}
	first := parent180WxPay(t, f, setup.id, "default-parent236-history-query")
	if first["code"] != float64(1) {
		t.Fatal("original default parent collection failed", first)
	}
	if out := parent180WxPay(t, f, setup.id, "default-parent236-history-callback"); out["code"] != float64(1) {
		t.Fatal("original callback order was not created", out)
	}
	queryOrder, err := f.s.Store.Order(t.Context(), 1, "default-parent236-history-query")
	if err != nil {
		t.Fatal(err)
	}
	callbackOrder, err := f.s.Store.Order(t.Context(), 1, "default-parent236-history-callback")
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.s.Store.ProviderAttempt(t.Context(), queryOrder.TradeNo)
	if err != nil || before.Material == "" {
		t.Fatal("original payment material absent", err)
	}
	if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false') WHERE resource='pay-rolls' AND id=$1`, rollID); err != nil {
		t.Fatal(err)
	}
	again := parent180WxPay(t, f, setup.id, "default-parent236-history-query")
	// The outer checkout URL may refresh its expiry/token. The sealed SDK
	// material below, its attempt ID and the upstream send count must not.
	if again["code"] != float64(1) || again["trade_no"] != first["trade_no"] || setup.creates.Load() != 2 {
		t.Fatal("closed current roll blocked or resent a sealed original payment", again, setup.creates.Load())
	}
	after, err := f.s.Store.ProviderAttempt(t.Context(), queryOrder.TradeNo)
	if err != nil || after.ID != before.ID || after.Material != before.Material {
		t.Fatal("sealed payment material changed on replay", err)
	}
	if out := parent180WxPay(t, f, setup.id, "default-parent236-new-after-roll-close"); out["code"] == float64(1) || setup.creates.Load() != 2 {
		t.Fatal("closed roll authorized a new payment", out)
	}
	parent180RotateOutgoing(t, setup)
	f.s.DefaultPaymentChannels = nil
	if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=body||'{"enabled":false,"info":"invalid-current-routing"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ProviderFlow.Query(t.Context(), 1, queryOrder.TradeNo); err != nil {
		t.Fatal("query failed to restore the sealed original account", err)
	}
	if _, err := f.s.ProviderFlow.Refund(t.Context(), 1, queryOrder.TradeNo, "default-parent236-original-refund", 1000); err != nil {
		t.Fatal("refund failed to use the sealed original account", err)
	}
	callback := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "synthetic180-fresh", "mch_id": "synthetic180-fresh-mch", "out_trade_no": callbackOrder.TradeNo, "transaction_id": "WX180_" + callbackOrder.TradeNo, "total_fee": "10000", "trade_type": "NATIVE", "openid": "synthetic180-buyer"}
	sendCallback := func(key string) int {
		r := httptest.NewRequest("POST", "/callbacks/providers/"+setup.id+"/"+callbackOrder.TradeNo, bytes.NewReader(parent180SignedXML(callback, key)))
		r.Header.Set("Content-Type", "application/xml")
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if code := sendCallback(setup.keyB); code != 400 {
		t.Fatal("current key authenticated a sealed original callback", code)
	}
	if code := sendCallback("abcdef0123456789abcdef0123456789"); code != 200 {
		t.Fatal("valid original callback failed after current routing closed", code)
	}
	paid, err := f.s.Store.Order(t.Context(), 1, callbackOrder.OutTradeNo)
	if err != nil || paid.Status != "paid" || setup.creates.Load() != 2 || setup.queries.Load() != 1 || setup.refunds.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("historical operations changed the original TLS/MAC account or repeated a send", paid.Status, setup.creates.Load(), setup.queries.Load(), setup.refunds.Load(), setup.bad.Load(), err)
	}
}

func TestDefaultPaymentParent236PreauthorizationNeedsExplicitMerchantAdmission(t *testing.T) {
	f := channel168Local(t)
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	origin := channelconfig.Channel{ID: 8, Plugin: "alipay", AppTypes: []string{"3", "5"}, Config: map[string]string{"appid": "synthetic-default236-preauth", "appmchid": "synthetic-default236-mch", "appsecret": parent180Private(merchant), "appkey": parent180Public(platform), "sign_type": "0"}}
	factory, err := channelconfig.NewParentChannelFactory(origin, channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}})
	if err != nil {
		t.Fatal(err)
	}
	execution, _, err := factory.BootstrapSnapshot(8)
	if err != nil || !slices.Contains(execution.Payment.Capabilities().Methods, "preauth") {
		t.Fatal("real preauthorization product fixture unavailable", err)
	}
	f.s.ProviderFlow.Providers[8] = execution.Payment
	f.s.ProviderFlow.ParentFactories = map[int64]adapter.ParentAccountFactory{8: factory}
	delete(f.s.MerchantChannels, 1)
	f.s.DefaultPaymentChannels = []int64{8}
	view, err := f.s.withParentRuntime(t.Context(), 1, "", "alipay")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := view.providerRoute(1, compat.Params{"type": "alipay", "channel_id": "8", "method": "preauth"}); err == nil {
		t.Fatal("default-only merchant acquired an unsupported capture lifecycle")
	}
	f.s.MerchantChannels[1] = []int64{8}
	view, err = f.s.withParentRuntime(t.Context(), 1, "", "alipay")
	if err != nil {
		t.Fatal(err)
	}
	if id, method, err := view.providerRoute(1, compat.Params{"type": "alipay", "channel_id": "8", "method": "preauth"}); err != nil || id != 8 || method != "preauth" {
		t.Fatal("explicit preauthorization admission changed", fmt.Sprint(id), method, err)
	}
}
