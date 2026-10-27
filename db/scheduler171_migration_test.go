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

func TestScheduler171MigrationKeepsAllOldFactsAndNeverEnablesMetadata(t *testing.T) {
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
	schema := fmt.Sprintf("scheduler171_upgrade_%d", time.Now().UnixNano())
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
	if _, e = p.Exec(ctx, `CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); e != nil {
		t.Fatal(e)
	}
	entries, e := files.ReadDir("migrations")
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if entry.Name() >= "000100_" {
			continue
		}
		raw, e := files.ReadFile("migrations/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = p.Exec(ctx, string(raw)); e != nil {
			t.Fatal(entry.Name(), e)
		}
		if _, e = p.Exec(ctx, `INSERT INTO schema_migrations VALUES($1,$2,'2026-10-25T02:00:00Z')`, entry.Name(), fmt.Sprintf("%x", sha256.Sum256(raw))); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = p.Exec(ctx, `INSERT INTO merchants(id,name,api_key,signature_algorithm,enabled,config_revision,profile)VALUES(1,'synthetic171-history',decode(repeat('ab',48),'hex'),'MD5',true,31,'{"original":"preserve"}');INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee)VALUES('171-old-trade',1,'171-old-out','old original','alipay',125,124,1);INSERT INTO backoffice_records(resource,body)VALUES('cron-tasks','{"name":"enabled legacy","kind":"payment.notify","interval_seconds":60,"enabled":true}');INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',1,'synthetic171','cron-tasks',1,'old-config','{"proof":"keep"}')`); e != nil {
		t.Fatal(e)
	}
	columns := map[string][]string{}
	rows, e := p.Query(ctx, `SELECT table_name,column_name FROM information_schema.columns WHERE table_schema=current_schema()ORDER BY table_name,ordinal_position`)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var table, col string
		if e = rows.Scan(&table, &col); e != nil {
			t.Fatal(e)
		}
		columns[table] = append(columns[table], pgx.Identifier{col}.Sanitize())
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		t.Fatal(e)
	}
	snapshot := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		for table, cols := range columns {
			where := ""
			if table == "schema_migrations" {
				where = " WHERE name<'000100_'"
			}
			var value string
			if e := p.QueryRow(ctx, `SELECT md5(COALESCE(string_agg(row::text,E'\n'ORDER BY row::text),''))FROM(SELECT to_jsonb(r)AS row FROM(SELECT `+strings.Join(cols, ",")+` FROM `+pgx.Identifier{table}.Sanitize()+where+`)r)x`).Scan(&value); e != nil {
				t.Fatal(table, e)
			}
			out[table] = value
		}
		return out
	}
	before := snapshot()
	for repeat := 0; repeat < 2; repeat++ {
		if e = Migrate(ctx, p); e != nil {
			t.Fatal("real registrar", repeat, e)
		}
		after := snapshot()
		for table, want := range before {
			if after[table] != want {
				t.Fatal("old row/column/proof/registry time changed", table)
			}
		}
		for _, table := range []string{"scheduled_tasks", "scheduled_task_runs", "scheduled_task_results", "scheduled_job_admissions", "scheduled_job_claims"} {
			var count int
			if e = p.QueryRow(ctx, `SELECT count(*)FROM `+table).Scan(&count); e != nil || count != 0 {
				t.Fatal("migration fabricated enabled tasks or execution history", table, count, e)
			}
		}
	}
	raw, e := files.ReadFile("migrations/000100_real_scheduled_tasks.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	var digest string
	if e = p.QueryRow(ctx, `SELECT sha256 FROM schema_migrations WHERE name='000100_real_scheduled_tasks.up.sql'`).Scan(&digest); e != nil || digest != fmt.Sprintf("%x", sha256.Sum256(raw)) {
		t.Fatal("actual migration100 registration/checksum", e)
	}
}
