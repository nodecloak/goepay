package db

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestProfile198MigrationPreservesAllOldColumnsAndMarksMissingImportTimes(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("actual PostgreSQL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := fmt.Sprintf("profile198_upgrade_%d", time.Now().UnixNano())
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
	tx, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); e != nil {
		t.Fatal(e)
	}
	entries, e := files.ReadDir("migrations")
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if entry.Name() >= "000112_" {
			continue
		}
		raw, e := files.ReadFile("migrations/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, string(raw)); e != nil {
			t.Fatal(entry.Name(), e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO schema_migrations(name,sha256)VALUES($1,$2)`, entry.Name(), fmt.Sprintf("%x", sha256.Sum256(raw))); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO merchants(id,name,api_key,profile,created_at,enabled) VALUES(1,'Native198',decode(repeat('ab',48),'hex'),'{}','2020-01-02T03:04:05Z',true),(2,'Pro198',decode(repeat('cd',48),'hex'),'{"legacy_source":"epay_pro","legacy_export":"actual-old-import198"}','2021-02-03T04:05:06Z',false),(3,'Release198',decode(repeat('ef',48),'hex'),'{"legacy_source":"epay_release","legacy_export":"actual-old-release198"}','2022-03-04T05:06:07Z',false),(4,'NativeHint198',decode(repeat('fa',48),'hex'),'{"legacy_source":"epay_pro"}','2023-04-05T06:07:08Z',true)`); e != nil {
		t.Fatal(e)
	}
	type oldTable struct{ name, columns, before string }
	tables := []oldTable{}
	rows, e := p.Query(ctx, `SELECT c.relname,string_agg(quote_ident(a.attname),',' ORDER BY a.attnum) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped WHERE n.nspname=current_schema() AND c.relkind='r' AND c.relname<>'schema_migrations' GROUP BY c.relname ORDER BY c.relname`)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var name, columns string
		if e = rows.Scan(&name, &columns); e != nil {
			t.Fatal(e)
		}
		tables = append(tables, oldTable{name: name, columns: columns})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		t.Fatal(e)
	}
	snapshot := func(table oldTable) string {
		t.Helper()
		var value string
		query := `SELECT COALESCE(jsonb_agg(to_jsonb(x)ORDER BY to_jsonb(x)::text),'[]'::jsonb)::text FROM(SELECT ` + table.columns + ` FROM ` + pgx.Identifier{table.name}.Sanitize() + `)x`
		if e := p.QueryRow(ctx, query).Scan(&value); e != nil {
			t.Fatal(table.name, e)
		}
		return value
	}
	for i := range tables {
		tables[i].before = snapshot(tables[i])
	}
	if e = Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	for _, table := range tables {
		if after := snapshot(table); after != table.before {
			t.Fatal("profile upgrade altered preexisting columns or history", table.name)
		}
	}
	var flags string
	if e = p.QueryRow(ctx, `SELECT string_agg(COALESCE(to_jsonb(m)->>'registration_time_known','missing'),','ORDER BY id)FROM merchants m WHERE id BETWEEN 1 AND 4`).Scan(&flags); e != nil || flags != "true,false,false,true" {
		t.Fatal("native or legacy registration provenance missing", flags, e)
	}
	if e = Migrate(ctx, p); e != nil {
		t.Fatal("upgrade not reentrant", e)
	}
	var unchanged int
	if e = p.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name LIKE '000112_%'`).Scan(&unchanged); e != nil || unchanged != 1 {
		t.Fatal("registration migration not registered exactly once", unchanged, e)
	}
}
