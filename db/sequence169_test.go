package db

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSequence169MigrationAddsNoInventedHistoryAndPreservesEveryOldTable(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := fmt.Sprintf("sequence169_upgrade_%d", time.Now().UnixNano())
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	entries, e := files.ReadDir("migrations")
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if entry.Name() >= "000096_" {
			continue //96 belongs to concurrent168A; final integration tests95→96→97.
		}
		raw, e := files.ReadFile("migrations/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = p.Exec(ctx, string(raw)); e != nil {
			t.Fatal(entry.Name(), e)
		}
	}
	if _, e = p.Exec(ctx, `INSERT INTO merchants(id,name,api_key) VALUES(1,'synthetic169-history',decode(repeat('ab',48),'hex')); INSERT INTO group_route_counters(group_id,cursor,assigned) VALUES(0,13,29); INSERT INTO provider_route_assignments(merchant_id,out_trade_no,channel_id,method,pay_type,amount,request_hash,created_at) VALUES(1,'synthetic169-old',7,'native','alipay',123,decode(repeat('cd',32),'hex'),'2026-10-25T02:00:00Z')`); e != nil {
		t.Fatal(e)
	}
	snapshot := func() map[string]string {
		t.Helper()
		rows, e := p.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename`)
		if e != nil {
			t.Fatal(e)
		}
		names := []string{}
		for rows.Next() {
			var name string
			if e = rows.Scan(&name); e != nil {
				t.Fatal(e)
			}
			names = append(names, name)
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		rows.Close()
		out := map[string]string{}
		for _, name := range names {
			var digest, columns string
			quoted := `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
			if e = p.QueryRow(ctx, `SELECT md5(COALESCE(string_agg(row::text,E'\n' ORDER BY row::text),'')) FROM (SELECT to_jsonb(r) AS row FROM `+quoted+` r) x`).Scan(&digest); e != nil {
				t.Fatal(name, e)
			}
			if e = p.QueryRow(ctx, `SELECT md5(string_agg(column_name||':'||data_type||':'||is_nullable||':'||COALESCE(column_default,''),E'\n' ORDER BY ordinal_position)) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1`, name).Scan(&columns); e != nil {
				t.Fatal(e)
			}
			out[name] = digest + columns
		}
		return out
	}
	before := snapshot()
	raw, e := files.ReadFile("migrations/000097_global_channel_last_used.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	after := snapshot()
	if len(after) != len(before)+1 {
		t.Fatal("unexpected business table changes", len(before), len(after))
	}
	for name, digest := range before {
		if after[name] != digest {
			t.Fatal("old columns/rows/financial proof changed", name)
		}
	}
	var count int64
	if e = p.QueryRow(ctx, `SELECT count(*) FROM provider_channel_last_used`).Scan(&count); e != nil || count != 0 {
		t.Fatal("historical route invented a new global use fact", count, e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO provider_channel_last_used(channel_id,merchant_id,out_trade_no,last_used_millis) VALUES(7,1,'synthetic169-old',1792893600000)`); e == nil {
		t.Fatal("non-sequential old route could invent a new fact")
	}
}
