package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

// The original refuses a pay type whose pre_type row is missing or has
// status 0 (epay_release includes/lib/Channel.php:115-116). A fresh original
// install has alipay and wxpay open and the other types closed; a 支付方式
// record opens or closes a type.
func TestPayTypeCatalogOpensAndClosesRouting(t *testing.T) {
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
	schema := "paytypes_" + hex.EncodeToString(nonce)
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
	var seeded int
	if e = scoped.QueryRow(ctx, `SELECT count(*) FROM backoffice_records WHERE resource='pay-types'`).Scan(&seeded); e != nil || seeded != 0 {
		t.Fatal("migrations must not add 支付方式 rows", seeded, e)
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
	// A ported 彩虹易支付 channel serving QQ钱包 (one channel, one pay type).
	qq, e := adapter.BuildPluginChannel(adapter.PluginChannel{ID: 9, Plugin: "epay", PayType: "qqpay", Config: map[string]string{"appurl": "https://pay.example.invalid/", "appid": "1000", "appkey": "synthetic-key", "appswitch": "1"}})
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{Pool: scoped, Store: store, CheckoutKey: bytes.Repeat([]byte{1}, 32), MerchantChannels: map[int64][]int64{1: {7, 9}}, ProviderFlow: &payment.ProviderFlow{Store: store, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid", Providers: map[int64]adapter.Provider{7: ali, 9: qq}}}
	params := func(kind, no string) compat.Params {
		return compat.Params{"type": kind, "method": "native", "name": "item", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": no}
	}
	record := func(name, kind string, on bool) {
		t.Helper()
		v := "false"
		if on {
			v = "true"
		}
		if _, e := scoped.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('pay-types',jsonb_build_object('name',$1::text,'type',$2::text,'enabled',$3::jsonb))`, name, kind, v); e != nil {
			t.Fatal(e)
		}
	}

	// Defaults of a fresh original install.
	if id, _, e := s.providerRoutedRequest(ctx, 1, params("alipay", "ali-1")); e != nil || id != 7 {
		t.Fatal("alipay is open by default", id, e)
	}
	if _, _, e := s.providerRoutedRequest(ctx, 1, params("qqpay", "qq-1")); !errors.Is(e, errPayTypeClosed) {
		t.Fatal("qqpay is closed until enabled, like pre_type status 0", e)
	}
	// The administrator enables QQ钱包: the plugin channel serves it.
	record("QQ钱包", "qqpay", true)
	if id, _, e := s.providerRoutedRequest(ctx, 1, params("qqpay", "qq-2")); e != nil || id != 9 {
		t.Fatal("enabled qqpay routes to the plugin channel", id, e)
	}
	// A second, disabled record does not close a type that has an enabled one.
	record("QQ钱包备用", "qqpay", false)
	if id, _, e := s.providerRoutedRequest(ctx, 1, params("qqpay", "qq-3")); e != nil || id != 9 {
		t.Fatal("one enabled qqpay record keeps it open", id, e)
	}
	if _, e = scoped.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false'::jsonb) WHERE resource='pay-types' AND body->>'type'='qqpay'`); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.providerRoutedRequest(ctx, 1, params("qqpay", "qq-4")); !errors.Is(e, errPayTypeClosed) {
		t.Fatal("all qqpay records disabled", e)
	}
	// Closing alipay with a disabled record, then removing the record again.
	record("支付宝", "alipay", false)
	if _, _, e := s.providerRoutedRequest(ctx, 1, params("alipay", "ali-2")); !errors.Is(e, errPayTypeClosed) {
		t.Fatal("closed alipay", e)
	}
	if _, e = scoped.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='pay-types' AND body->>'type'='alipay'`); e != nil {
		t.Fatal(e)
	}
	if id, _, e := s.providerRoutedRequest(ctx, 1, params("alipay", "ali-3")); e != nil || id != 7 {
		t.Fatal("alipay back to its default", id, e)
	}
	// Not in the original catalog at all: refused before any lookup.
	if _, _, e := s.providerRoutedRequest(ctx, 1, params("fakepay", "fake-1")); e == nil || errors.Is(e, errPayTypeClosed) {
		t.Fatal("unknown pay type", e)
	}
}

func TestPayTypeCatalogDefaultsMatchTheOriginalInstall(t *testing.T) {
	want := map[string]bool{"alipay": true, "wxpay": true, "qqpay": false, "bank": false, "jdpay": false, "paypal": false, "douyinpay": false, "ecny": false, "fakepay": false}
	for kind, open := range want {
		if payTypeDefaultOpen(kind) != open {
			t.Fatal(kind, open)
		}
	}
	for _, p := range originalPayTypes {
		if !adapter.CatalogPayType(p.Name) || p.ShowName == "" {
			t.Fatal("catalog entry", p.Name)
		}
	}
}
