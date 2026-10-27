package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSignature163MigrationPreservesEveryExistingRow(t *testing.T) {
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
	schema := fmt.Sprintf("signature163_upgrade_%d", time.Now().UnixNano())
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
		if strings.HasPrefix(entry.Name(), "000095_") {
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
	if _, e = p.Exec(ctx, `INSERT INTO merchants(id,name,api_key,signature_algorithm,rsa_public_key,enabled,config_revision,profile) SELECT i,'local-old-'||i,decode(repeat('ab',48),'hex'),CASE WHEN i%2=0 THEN 'RSA' ELSE 'MD5' END,'legacy-public-'||i,i%3<>0,13+i,jsonb_build_object('private_migration_proof',i) FROM generate_series(1,9) i; INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',1,'old-source','merchants',1,'old-proof','{"evidence":"preserve"}')`); e != nil {
		t.Fatal(e)
	}
	snapshot := func() map[string]string {
		t.Helper()
		names, e := p.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename`)
		if e != nil {
			t.Fatal(e)
		}
		var tables []string
		for names.Next() {
			var name string
			if e = names.Scan(&name); e != nil {
				t.Fatal(e)
			}
			tables = append(tables, name)
		}
		if e = names.Err(); e != nil {
			t.Fatal(e)
		}
		names.Close()
		out := map[string]string{}
		for _, name := range tables {
			var digest string
			expr := "to_jsonb(r)"
			if name == "merchants" {
				expr = "to_jsonb(r)-'keytype'"
			}
			if e = p.QueryRow(ctx, `SELECT md5(COALESCE(string_agg(row::text,E'\n' ORDER BY row::text),'')) FROM (SELECT `+expr+` AS row FROM "`+name+`" r) x`).Scan(&digest); e != nil {
				t.Fatal(e)
			}
			out[name] = digest
		}
		return out
	}
	before := snapshot()
	raw, e := files.ReadFile("migrations/000095_merchant_signature_policy.up.sql")
	if e != nil {
		t.Fatal("signature policy upgrade unavailable", e)
	}
	if _, e = p.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	after := snapshot()
	if len(after) != len(before) {
		t.Fatal("migration added/dropped business table")
	}
	for name, digest := range before {
		if after[name] != digest {
			t.Fatal("migration changed old row/proof", name)
		}
	}
	var nulls, total int
	var def *string
	if e = p.QueryRow(ctx, "SELECT count(*) FILTER (WHERE keytype IS NULL),count(*) FROM merchants").Scan(&nulls, &total); e != nil || nulls != 9 || total != 9 {
		t.Fatal("old rows did not retain strict null policy", nulls, total, e)
	}
	if e = p.QueryRow(ctx, "SELECT column_default FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='merchants' AND column_name='keytype'").Scan(&def); e != nil || def != nil {
		t.Fatal("policy default is not null", e)
	}
	if _, e = p.Exec(ctx, "UPDATE merchants SET keytype=2 WHERE id=1"); e == nil {
		t.Fatal("invalid policy persisted")
	}
	for _, value := range []string{"0", "1", "NULL"} {
		if _, e = p.Exec(ctx, "UPDATE merchants SET keytype="+value+" WHERE id=1"); e != nil {
			t.Fatal("valid policy refused", value, e)
		}
	}
}
