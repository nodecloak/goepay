package db

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise registered105 from original101; preserve every original column and row.
func TestOrderBatch185MigrationPreservesOriginal101FactsAndNeverInventsBatchIntents(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("parity185_upgrade_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, "CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		t.Fatal(err)
	}
	entries, err := files.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "000105_" {
			continue
		}
		raw, err := files.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, string(raw)); err != nil {
			t.Fatal(entry.Name(), err)
		}
		if _, err = pool.Exec(ctx, "INSERT INTO schema_migrations(name,sha256,applied_at) VALUES($1,$2,'2026-10-25T02:00:00Z')", entry.Name(), fmt.Sprintf("%x", sha256.Sum256(raw))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO merchants(id,name,api_key,signature_algorithm,keytype,enabled,config_revision,profile) VALUES(1,'synthetic185-history',decode(repeat('ab',48),'hex'),'MD5',NULL,true,23,'{"original":"unchanged"}');
INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee) VALUES('185-old-trade',1,'185-old-out','original order facts','alipay',125,124,1);
INSERT INTO group_route_counters(group_id,cursor,assigned) VALUES(0,13,29);
INSERT INTO provider_route_assignments(merchant_id,out_trade_no,channel_id,method,pay_type,amount,request_hash,created_at) VALUES(1,'185-old-out',7,'native','alipay',125,decode(repeat('cd',32),'hex'),'2026-10-25T02:00:00Z');
INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',1,'synthetic185','channels',7,'old-proof','{"proof":"preserve"}')`); err != nil {
		t.Fatal(err)
	}
	columns := map[string][]string{}
	digests := func(capture bool) map[string]string {
		t.Helper()
		if capture {
			rows, err := pool.Query(ctx, `SELECT table_name,column_name FROM information_schema.columns WHERE table_schema=current_schema() ORDER BY table_name,ordinal_position`)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var table, column string
				if err = rows.Scan(&table, &column); err != nil {
					t.Fatal(err)
				}
				columns[table] = append(columns[table], pgx.Identifier{column}.Sanitize())
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
		}
		out := map[string]string{}
		for table, cols := range columns {
			predicate := ""
			if table == "schema_migrations" {
				predicate = " WHERE name<'000105_'"
			}
			query := `SELECT md5(COALESCE(string_agg(row::text,E'\n' ORDER BY row::text),'')) FROM (SELECT to_jsonb(r) AS row FROM (SELECT ` + strings.Join(cols, ",") + ` FROM ` + pgx.Identifier{table}.Sanitize() + predicate + `) r) x`
			var digest string
			if err := pool.QueryRow(ctx, query).Scan(&digest); err != nil {
				t.Fatal(table, err)
			}
			out[table] = digest
		}
		return out
	}
	before := digests(true)
	for pass := 0; pass < 2; pass++ {
		if err = Migrate(ctx, pool); err != nil {
			t.Fatal("registered full chain", pass, err)
		}
		after := digests(false)
		for table, want := range before {
			if after[table] != want {
				t.Fatal("old original columns/rows changed", pass, table)
			}
		}
		for _, name := range []string{"000105_order_business_batches.up.sql"} {
			raw, err := files.ReadFile("migrations/" + name)
			if err != nil {
				t.Fatal(err)
			}
			var got string
			if err = pool.QueryRow(ctx, "SELECT sha256 FROM schema_migrations WHERE name=$1", name).Scan(&got); err != nil || got != fmt.Sprintf("%x", sha256.Sum256(raw)) {
				t.Fatal("registered checksum", name, got, err)
			}
		}
		for _, table := range []string{"order_business_batches", "order_business_batch_items", "order_business_batch_events"} {
			var count int
			if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
				t.Fatal("invented historical fact", table, count, err)
			}
		}
		var marked int
		if err = pool.QueryRow(ctx, "SELECT count(*) FROM orders WHERE runtime_fee_quote_required").Scan(&marked); err != nil || marked != 0 {
			t.Fatal("invented historical pricing requirement", marked, err)
		}
	}
}
