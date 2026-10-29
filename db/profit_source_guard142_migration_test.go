package db

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 000142: order_profit_source_guard() keeps 000055's channel_sharing_fund_releases
// check (a finish also releases a channel-sharing hold) together with 000136's
// per-recipient external_no rules, and the migration is registered once.
func TestProfitSourceGuard142RestoresFundReleaseCheck(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("actual PostgreSQL required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("profit_guard142_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("migrate", err)
	}
	if err = VerifySchema(ctx, p); err != nil {
		t.Fatal(err)
	}
	var def string
	if err = p.QueryRow(ctx, `SELECT pg_get_functiondef(p.oid) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE p.proname='order_profit_source_guard' AND n.nspname=$1`, schema).Scan(&def); err != nil {
		t.Fatal("guard missing:", err)
	}
	if !strings.Contains(def, "channel_sharing_fund_releases") || strings.Contains(def, "channel_sharing_completions") {
		t.Fatal("guard must check channel_sharing_fund_releases, not channel_sharing_completions")
	}
	if !strings.Contains(def, ":recipient:") || !strings.Contains(def, "request.recipient_id<>NEW.recipient_id") {
		t.Fatal("guard lost the per-recipient rules from 000136")
	}
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("reentry", err)
	}
	var count int
	if err = p.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name='000142_profit_source_guard_fund_releases.up.sql'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration must register exactly once: %d %v", count, err)
	}
}
