package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOrderIncomeChannelSharingUpgradeKeepsLegacyHoldProof(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	nonce := make([]byte, 8)
	if _, e = rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	schema := "income_upgrade_" + hex.EncodeToString(nonce)
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		admin.Close()
	})
	paths, e := filepath.Glob("../../db/migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range paths {
		if strings.Compare(filepath.Base(p), "000052_") >= 0 {
			continue
		}
		body, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(body)); e != nil {
			t.Fatal(filepath.Base(p), e)
		}
	}
	// Current payment Go scans the independent purpose column introduced by 54.
	// Apply that independent metadata migration before generating pre-52 holds;
	// the old hold schema and all its original source journals remain unchanged.
	purpose, e := os.ReadFile("../../db/migrations/000054_channel_sharing_payment_purpose.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(purpose)); e != nil {
		t.Fatal(e)
	}
	s, e := NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CreateMerchant(ctx, Merchant{ID: 751, Name: "legacy income", APIKey: "local-upgrade-only"}); e != nil {
		t.Fatal(e)
	}
	// Produce a genuine pre-52 payment and journal without invoking current
	// CreateOrder/MarkPaid, whose referral snapshots require newer migrations.
	paidTx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer paidTx.Rollback(ctx)
	o, e := scanOrder(paidTx.QueryRow(ctx, "INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount) VALUES(nextval('trade_no_seq')::text,751,'legacy-income-proof','item','',100) RETURNING "+orderColumns))
	if e != nil {
		t.Fatal(e)
	}
	if e = post(ctx, paidTx, "payment:"+o.TradeNo, "historical payment net credit", 0, "clearing", 751, "available", 100); e != nil {
		t.Fatal(e)
	}
	if _, e = paidTx.Exec(ctx, "UPDATE orders SET status='paid',paid_at=statement_timestamp(),provider_event_id='paid-legacy-income-proof' WHERE id=$1", o.ID); e != nil {
		t.Fatal(e)
	}
	if e = paidTx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var id int64
	if e = tx.QueryRow(ctx, "INSERT INTO order_income_holds(order_id,merchant_id,request_no,merchant_net,refund_merchant_amount,amount) VALUES($1,751,'legacy-freeze',100,0,100) RETURNING id", o.ID).Scan(&id); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO accounts(owner_id,kind) VALUES(751,'order_income_hold')"); e != nil {
		t.Fatal(e)
	}
	if e = post(ctx, tx, "order-income:"+fmt.Sprint(id)+":freeze", "legacy original freeze", 751, "available", 751, "order_income_hold", 100); e != nil {
		t.Fatal(e)
	}
	if e = incomeHoldAudit(ctx, tx, OrderIncomeHold{ID: id, OrderID: o.ID, MerchantID: 751, TradeNo: o.TradeNo, RequestNo: "legacy-freeze", Amount: 100, State: "held"}, "freeze", incomeTestActor); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	migration, e := os.ReadFile("../../db/migrations/000052_order_income_channel_sharing.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(migration)); e != nil {
		t.Fatal(e)
	}
	h, e := s.OrderIncomeHold(ctx, 751, o.TradeNo)
	if e != nil || h.EvidenceVersion != 1 || h.ChannelSharingAmount != 0 || h.InternalAllocatedAmount != 0 || h.Amount != 100 {
		t.Fatal(h, e)
	}
	// Continue to the current independent PF/PR schema before invoking current
	// Go methods, which now use the unified release projection. The version1
	// assertion above is deliberately made immediately after migration52.
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.Compare(name, "000053_") < 0 || strings.HasPrefix(name, "000054_") {
			continue
		}
		body, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(body)); e != nil {
			t.Fatal(name, e)
		}
	}
	if _, e = s.UnfreezeOrderIncome(ctx, 751, o.TradeNo, h.ID, incomeTestActor); e != nil {
		t.Fatal("legacy proof was not releasable", e)
	}
	h, e = s.FreezeOrderIncome(ctx, 751, o.TradeNo, "new-proof", incomeTestActor)
	if e != nil || h.EvidenceVersion != 2 || h.ChannelSharingAmount != 0 || h.InternalAllocatedAmount != 0 || h.Amount != 100 {
		t.Fatal(h, e)
	}
}
