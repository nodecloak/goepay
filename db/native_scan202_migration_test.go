package db

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNativeScan202Migration114PreservesOriginalResourcesAndRowsAndIsReentrant(t *testing.T) {
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
	schema := fmt.Sprintf("native_scan202_upgrade_%d", time.Now().UnixNano())
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
	if _, err = p.Exec(ctx, `CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	entries, err := files.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "000114_" {
			continue
		}
		raw, err := files.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = p.Exec(ctx, string(raw)); err != nil {
			t.Fatal(entry.Name(), err)
		}
		if _, err = p.Exec(ctx, `INSERT INTO schema_migrations(name,sha256)VALUES($1,$2)`, entry.Name(), fmt.Sprintf("%x", sha256.Sum256(raw))); err != nil {
			t.Fatal(err)
		}
	}
	// Literal original catalogue, independently checked from the historical
	// public schema. Exercise insert permission rather than inspecting SQL text.
	oldResources := []string{"groups", "domains", "invitation-codes", "announcements", "settings", "channels", "blacklist", "collection-styles", "profitsharing-rules", "pay-types", "pay-rolls", "weixin-apps", "onboarding-channels", "onboarding-applications", "subchannels", "merchant-risk", "complaints", "complaint-tasks", "risk-records", "cron-tasks", "settlement-settings", "collection-codes", "plugin-configs", "template-configs", "addon-configs", "order-subject-policies"}
	for _, resource := range oldResources {
		if _, err = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body,created_at,updated_at)VALUES($1,jsonb_build_object('key','native202-old-'||$1::text,'name','native202-old-'||$1::text,'original_proof','preserve-'||$1::text),'2020-01-02T03:04:05Z','2021-02-03T04:05:06Z')`, resource); err != nil {
			t.Fatal("historical resource fixture rejected", resource, err)
		}
	}
	if _, err = p.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes,created_at)VALUES('admin',202,'historical-native202','settings',202,'original-audit','{"original_proof":"keep-exactly"}','2020-01-02T03:04:05Z')`); err != nil {
		t.Fatal(err)
	}
	assertRejected := func(resource string) {
		t.Helper()
		_, err := p.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES($1,'{}')`, resource)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "23514" || pgerr.ConstraintName != "backoffice_records_resource_check" {
			t.Fatal("resource catalogue failed to reject unknown resource", resource, err)
		}
	}
	assertRejected("scan-js-settings202")
	assertRejected("unknown-native202-resource")
	snapshot := func(includeRegistry bool) map[string]string {
		t.Helper()
		rows, err := p.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname=current_schema()AND($1 OR tablename<>'schema_migrations')ORDER BY tablename`, includeRegistry)
		if err != nil {
			t.Fatal(err)
		}
		names := []string{}
		for rows.Next() {
			var name string
			if err = rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, name := range names {
			var value string
			if err = p.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(t)ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM `+pgx.Identifier{name}.Sanitize()+` t`).Scan(&value); err != nil {
				t.Fatal(name, err)
			}
			out[name] = value
		}
		return out
	}
	before := snapshot(false)
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("upgrade113 to114", err)
	}
	// Later migrations may add empty tables. Every previously existing table,
	// row and proof byte must still be identical after the complete upgrade.
	after := snapshot(false)
	for name, value := range before {
		if after[name] != value {
			t.Fatal("native scan migration altered historical rows or proof bytes", name)
		}
	}
	if err = VerifySchema(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, resource := range oldResources {
		tx, err := p.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES($1,'{}')`, resource)
		rollbackErr := tx.Rollback(ctx)
		if err != nil || rollbackErr != nil {
			t.Fatal("original resource permission lost after upgrade", resource, err, rollbackErr)
		}
	}
	if _, err = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('scan-js-settings202','{"fixture":"native202-independent-source"}')`); err != nil {
		t.Fatal("new independent native setting resource remains rejected", err)
	}
	for _, resource := range []string{"unknown-native202-resource", "scan-js-settings202-typo", ""} {
		assertRejected(resource)
	}
	afterFirst := snapshot(true)
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("native scan migration reentry", err)
	}
	if afterSecond := snapshot(true); !reflect.DeepEqual(afterFirst, afterSecond) {
		t.Fatal("reentrant migrate changed old rows, new native source, or migration registry")
	}
	var count int
	if err = p.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name='000114_native_scan_js_settings202.up.sql'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("native scan migration must register exactly once", count, err)
	}
}
