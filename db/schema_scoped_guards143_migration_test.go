package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 000143: name-only catalog guards in 000131-000141 skip a trigger or CHECK in a
// schema when another schema of the same database already has an object with
// that name. Migrating two schemas one after the other reproduces it; after
// 000143 the second schema still has every guarded object on its own tables.
func TestSchemaScopedGuards143RepairSecondSchema(t *testing.T) {
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
	open := func(label string) *pgxpool.Pool {
		schema := fmt.Sprintf("guards143_%s_%d", label, time.Now().UnixNano())
		if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE") })
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			t.Fatal(err)
		}
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
		p, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		if err = Migrate(ctx, p); err != nil {
			t.Fatal("migrate", label, err)
		}
		return p
	}
	_ = open("first")
	second := open("second")
	triggers := map[string]string{
		"enrollment_phones134":                "enrollment_phone134_immutable",
		"complaints133":                       "complaints133_forward",
		"verification_codes134":               "verification_codes134_forward",
		"sms_test_requests134":                "sms_test_requests134_forward",
		"connect_login_states135":             "connect_login_states135_guard",
		"connect_pending136":                  "connect_pending136_guard",
		"merchant_complaint_events201":        "merchant_complaint_events201_immutable",
		"merchant_complaint_event_claims201":  "merchant_complaint_event_claims201_immutable",
		"merchant_complaint_event_results201": "merchant_complaint_event_results201_immutable",
	}
	for table, trigger := range triggers {
		var n int
		if err := second.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid=$1::regclass AND tgname=$2`, table, trigger).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s.%s missing in the second schema: %d %v", table, trigger, n, err)
		}
	}
	var n int
	if err := second.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid='console_users'::regclass AND conname='console_users_permissions_array'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("console_users_permissions_array missing in the second schema: %d %v", n, err)
	}
	if err := Migrate(ctx, second); err != nil {
		t.Fatal("reentry", err)
	}
	if err := second.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name='000143_schema_scoped_guards.up.sql'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("migration must register exactly once: %d %v", n, err)
	}
}
