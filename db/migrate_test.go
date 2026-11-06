package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestMigrationReentrantAndChecksumGuard(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	for i := 0; i < 2; i++ {
		if e = Migrate(ctx, pool); e != nil {
			t.Fatal(e)
		}
	}
	if e = VerifySchema(ctx, pool); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "DELETE FROM schema_migrations WHERE name=(SELECT max(name) FROM schema_migrations)"); e != nil {
		t.Fatal(e)
	}
	if e = VerifySchema(ctx, pool); e == nil {
		t.Fatal("readiness accepted missing migration")
	}
	// Restore only this isolated fixture before checksum corruption below.
	if _, e = pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE; CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "UPDATE schema_migrations SET sha256='tampered'"); e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, pool); e == nil {
		t.Fatal("accepted changed applied migration")
	}
}

func TestEmbeddedChecksumsAreComputedOnce(t *testing.T) {
	first, err := embeddedChecksums()
	if err != nil || len(first) == 0 {
		t.Fatal("embedded migrations unreadable", err)
	}
	second, err := embeddedChecksums()
	if err != nil || reflect.ValueOf(first).UnsafePointer() != reflect.ValueOf(second).UnsafePointer() {
		t.Fatal("readiness re-hashed the embedded migrations", err)
	}
	entries, _ := files.ReadDir("migrations")
	if len(first) != len(entries) {
		t.Fatalf("checksums cover %d of %d migrations", len(first), len(entries))
	}
}
