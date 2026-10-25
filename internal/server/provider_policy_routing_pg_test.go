package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"os"
	"testing"
)

func TestPayRollPostgresChannelAndGroupQuotas(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	nonce := make([]byte, 8)
	rand.Read(nonce)
	schema := "routes_" + hex.EncodeToString(nonce)
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	config, _ := pgxpool.ParseConfig(dsn)
	config.ConnConfig.RuntimeParams["search_path"] = schema
	scoped, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { scoped.Close(); pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") }()
	if e = db.Migrate(ctx, scoped); e != nil {
		t.Fatal(e)
	}
	store, e := payment.NewWithKey(scoped, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	if e = store.CreateMerchant(ctx, payment.Merchant{ID: 1, Name: "merchant", APIKey: "test-secret"}); e != nil {
		t.Fatal(e)
	}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"native"}})
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{Pool: scoped, Store: store, CheckoutKey: bytes.Repeat([]byte{1}, 32), MerchantChannels: map[int64][]int64{1: {7, 8}}, ProviderFlow: &payment.ProviderFlow{Store: store, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid", Providers: map[int64]adapter.Provider{7: ali, 8: ali}}}
	_, e = scoped.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('pay-rolls','{"name":"limits","merchant_id":"1","type":"alipay","strategy":"first","channel_ids":"7:1,8:1","enabled":true,"limits":"{\"daily_count\":2,\"daily_amount\":\"2.00\"}","rules":"{\"7\":{\"rate_limit_seconds\":60,\"rate_limit_count\":1},\"8\":{\"rate_limit_seconds\":60,\"rate_limit_count\":1}}"}')`)
	if e != nil {
		t.Fatal(e)
	}
	for i, want := range []int64{7, 8} {
		p := compat.Params{"type": "alipay", "method": "native", "name": "item", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": string(rune('a' + i))}
		got, _, e := s.providerRoutedRequest(ctx, 1, p)
		if e != nil || got != want {
			t.Fatal("quota selection", i, got, e)
		}
	}
	if _, _, e = s.providerRoutedRequest(ctx, 1, compat.Params{"type": "alipay", "method": "native", "name": "item", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "third"}); e == nil {
		t.Fatal("quota exceeded")
	}
	// The immutable database binding also guards direct Flow callers.
	o, e := store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "a", Type: "alipay", Name: "item", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = store.BeginProviderAttemptWithIdentity(ctx, o.TradeNo, 8, "native", "app", ""); e == nil {
		t.Fatal("database allowed attempt on a different reserved channel")
	}
	a, owner, e := store.BeginProviderAttemptWithIdentity(ctx, o.TradeNo, 7, "native", "app", "")
	if e != nil || !owner {
		t.Fatal(a, owner, e)
	}
	if e = store.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
		t.Fatal(e)
	}
	// Older attempts created before routing rollout remain authoritative even
	// if a new policy or an exhausted quota would now prefer another channel.
	old, e := store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "legacy-attempt", Type: "alipay", Name: "item", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if _, owner, e = store.BeginProviderAttemptWithIdentity(ctx, old.TradeNo, 8, "native", "app", ""); e != nil || !owner {
		t.Fatal(owner, e)
	}
	params := compat.Params{"type": "alipay", "method": "native", "name": "item", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "legacy-attempt"}
	if id, _, e := s.providerRoutedRequest(ctx, 1, params); e != nil || id != 8 {
		t.Fatal("legacy attempt was rerouted", id, e)
	}
	var legacyRows int
	if e = scoped.QueryRow(ctx, "SELECT count(*) FROM provider_route_assignments WHERE out_trade_no='legacy-attempt'").Scan(&legacyRows); e != nil || legacyRows != 0 {
		t.Fatal("unverified current request backfilled a historical attempt identity", legacyRows, e)
	}
	prepared, e := store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "legacy-oauth", Type: "alipay", Name: "item", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = store.PrepareOAuthPayment(ctx, payment.OAuthBinding{TradeNo: prepared.TradeNo, MerchantID: 1, ChannelID: 8, Provider: "alipay", AppID: "app", Method: "jsapi", Purpose: "payment-jsapi"}); e != nil {
		t.Fatal(e)
	}
	params["out_trade_no"] = "legacy-oauth"
	if _, _, e = s.providerRoutedRequest(ctx, 1, params); e == nil {
		t.Fatal("legacy prepared OAuth silently switched payment product")
	}
	if _, e = scoped.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(jsonb_set(body,'{channel_ids}','"7:1"'),'{rules}','"{}"') WHERE resource='pay-rolls'`); e != nil {
		t.Fatal(e)
	}
	params["out_trade_no"] = "b"
	if _, _, e = s.providerRoutedRequest(ctx, 1, params); e == nil {
		t.Fatal("unsubmitted binding survived removal from its rule")
	}
	if _, e = scoped.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{channel_ids}','"7:1,8:1"') WHERE resource='pay-rolls'`); e != nil {
		t.Fatal(e)
	}
	// A route committed before any attempt cannot silently move after disable.
	if _, e = scoped.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false') WHERE resource='pay-rolls'`); e != nil {
		t.Fatal(e)
	}
	params["out_trade_no"] = "b"
	if _, _, e = s.providerRoutedRequest(ctx, 1, params); e == nil {
		t.Fatal("disabled unsubmitted binding was silently reused")
	}
	params["out_trade_no"] = "a"
	if id, _, e := s.providerRoutedRequest(ctx, 1, params); e != nil || id != 7 {
		t.Fatal("disabled roll lost existing unknown attempt", id, e)
	}
	preauth, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"preauth"}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow.Providers[7] = preauth
	if _, e = scoped.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(jsonb_set(jsonb_set(body,'{enabled}','true'),'{strategy}','"first"'),'{limits}','"{}"') WHERE resource='pay-rolls'`); e != nil {
		t.Fatal(e)
	}
	params["out_trade_no"] = "browser-only"
	params["method"] = "web"
	params["allow_nonbrowser"] = "true"
	if id, method, e := s.providerRoutedBrowserRequest(ctx, 1, params); e != nil || id != 8 || method != "native" {
		t.Fatal("browser entry selected authorization or trusted caller mode", id, method, e)
	}
	params["out_trade_no"] = "ordinary-products"
	if id, method, e := s.providerRoutedRequest(ctx, 1, params); e != nil || id != 7 || method != "preauth" {
		t.Fatal("browser restriction leaked into ordinary payments", id, method, e)
	}
}
