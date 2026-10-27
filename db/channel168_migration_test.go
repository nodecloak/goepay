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

func TestChannel168MigrationPreservesEveryOldRowAndDoesNotBackfillPricing(t *testing.T) {
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
	schema := fmt.Sprintf("channel168_upgrade_%d", time.Now().UnixNano())
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
			continue
		}
		raw, e := files.ReadFile("migrations/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = p.Exec(ctx, string(raw)); e != nil {
			t.Fatal(entry.Name(), e)
		}
	}
	if _, e = p.Exec(ctx, `INSERT INTO merchants(id,name,api_key,signature_algorithm,keytype,enabled,config_revision,profile) SELECT i,'old168-'||i,decode(repeat('ab',48),'hex'),'MD5',CASE WHEN i%2=0 THEN 0 ELSE NULL END,i%3<>0,13+i,jsonb_build_object('oldproof',i) FROM generate_series(1,9) i;
 INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee) VALUES('168-old-trade',1,'168-old-out','old original','alipay',123,123,0);
 INSERT INTO provider_route_assignments(merchant_id,out_trade_no,channel_id,method,pay_type,amount,request_hash) VALUES(1,'168-old-out',7,'native','alipay',123,decode(repeat('ab',32),'hex')),(1,'168-old-pending-without-order',8,'native','alipay',123,decode(repeat('cd',32),'hex'));
 INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',1,'old-source','channels',7,'oldproof','{"evidence":"preserve"}')`); e != nil {
		t.Fatal(e)
	}
	// Capture all pre-upgrade columns, then compare every old column in every
	// row. New columns must not hide any old proof or silently reframe history.
	oldColumns := map[string][]string{}
	snapshot := func() map[string]string {
		t.Helper()
		rows, e := p.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename")
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
		rows.Close()
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		out := map[string]string{}
		for _, name := range names {
			columns, known := oldColumns[name]
			if !known {
				cr, e := p.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 ORDER BY ordinal_position`, name)
				if e != nil {
					t.Fatal(e)
				}
				for cr.Next() {
					var col string
					if e = cr.Scan(&col); e != nil {
						t.Fatal(e)
					}
					columns = append(columns, pgx.Identifier{col}.Sanitize())
				}
				cr.Close()
				if e = cr.Err(); e != nil {
					t.Fatal(e)
				}
				oldColumns[name] = columns
			}
			var digest string
			if e = p.QueryRow(ctx, `SELECT md5(COALESCE(string_agg(row::text,E'\n' ORDER BY row::text),'')) FROM (SELECT to_jsonb(r) AS row FROM (SELECT `+strings.Join(columns, ",")+` FROM `+pgx.Identifier{name}.Sanitize()+`) r) x`).Scan(&digest); e != nil {
				t.Fatal(e)
			}
			out[name] = digest
		}
		return out
	}
	before := snapshot()
	raw, e := files.ReadFile("migrations/000096_runtime_channel_fees.up.sql")
	if e != nil {
		t.Fatal("runtime channel migration unavailable", e)
	}
	if _, e = p.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	after := snapshot()
	for name, digest := range before {
		if after[name] != digest {
			t.Fatal("upgrade rewrote old row/column/proof", name)
		}
	}
	for _, table := range []string{"runtime_channel_policies", "provider_route_fee_quotes", "order_route_fee_terms"} {
		if _, exists := before[table]; exists {
			t.Fatal("test was not pre96")
		}
		var count int
		if e = p.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil || count != 0 {
			t.Fatal("upgrade fabricated historical fees", table, count, e)
		}
	}
	var marked int
	if e = p.QueryRow(ctx, "SELECT count(*) FROM orders WHERE runtime_fee_quote_required").Scan(&marked); e != nil || marked != 0 {
		t.Fatal("upgrade fabricated historical fee birth markers", marked, e)
	}
}
