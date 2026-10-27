package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buyer104BeforeMigrationStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("synthetic PostgreSQL required")
	}
	ctx := t.Context()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	nonce := make([]byte, 8)
	if _, e = rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	schema := "test_upgrade104_" + hex.EncodeToString(nonce)
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	scoped, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		scoped.Close()
		pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		pool.Close()
	})
	paths, e := filepath.Glob("../../db/migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range paths {
		if strings.HasSuffix(p, "000104_order_customer_charges.up.sql") {
			continue
		}
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = scoped.Exec(ctx, string(b)); e != nil {
			t.Fatal(p, e)
		}
	}
	s, e := NewWithKey(scoped, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	return s
}

func TestBuyerCharge104ActualMigrationPreservesOldNilAmountsAndMACBytes(t *testing.T) {
	s := buyer104BeforeMigrationStore(t)
	ctx := t.Context()
	const mid = 10434
	feeMerchant(t, s, mid, 50, "platform")
	// A pre104 imported birth uses the original retained-net rounding. Its old
	// terms/source/print commitments are authored while104 schema is absent.
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	o, e := scanOrder(tx.QueryRow(ctx, "INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee,fee_rate_bps,fee_policy,refund_fee_payer) VALUES('LEGACY104',$1,'legacy104','historical item','',100,100,0,50,'merchant_deduct','platform') RETURNING "+orderColumns, mid))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.snapshotReferralTerms(ctx, tx, o, "payment", nil); e != nil {
		t.Fatal(e)
	}
	if e = s.writePrintOrigin(ctx, tx, o, "payment", 0); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	old := func() (string, string, string) {
		t.Helper()
		var order, terms, origin string
		if e = s.pool.QueryRow(ctx, `SELECT to_jsonb(o)::text,encode(t.proof_hash,'hex'),encode(o.referral_source_hash,'hex') FROM orders o JOIN referral_order_terms t ON t.order_id=o.id WHERE o.id=$1`, o.ID).Scan(&order, &terms, &origin); e != nil {
			t.Fatal(e)
		}
		return order, terms, origin
	}
	before, termsBefore, originBefore := old()
	migration, e := os.ReadFile("../../db/migrations/000104_order_customer_charges.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(migration)); e != nil {
		t.Fatal(e)
	}
	var after, termsAfter, originAfter string
	if e = s.pool.QueryRow(ctx, `SELECT (to_jsonb(o)-'actual_charge_amount104'-'actual_charge_required104')::text,encode(t.proof_hash,'hex'),encode(o.referral_source_hash,'hex') FROM orders o JOIN referral_order_terms t ON t.order_id=o.id WHERE o.id=$1`, o.ID).Scan(&after, &termsAfter, &originAfter); e != nil || after != before || termsAfter != termsBefore || originAfter != originBefore {
		t.Fatal("migration changed historical financial values or proof bytes", e)
	}
	o, e = s.OrderByTradeNo(ctx, o.TradeNo)
	if e != nil {
		t.Fatal(e)
	}
	if o.actualChargeAmount104 != nil || o.actualChargeRequired104 {
		t.Fatal("old birth backfilled")
	}
	if c, e := s.AuthenticatedOrderCharge104(ctx, o); e != nil || c != 100 {
		t.Fatal("old private source failed authentication", c, e)
	}
	replay, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "legacy104", Name: "historical item", Amount: 100})
	if e != nil || replay.MerchantAmount != 100 || replay.PlatformFee != 0 {
		t.Fatal("old replay repriced", replay, e)
	}
	fresh, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "new-after104", Amount: 100})
	if e != nil || fresh.actualChargeAmount104 == nil || fresh.MerchantAmount != 99 || fresh.PlatformFee != 1 {
		t.Fatal("new birth silently retained old rounding", fresh, e)
	}
}
