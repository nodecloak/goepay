package db

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 000138: provider_transfer_attempts.method (plaintext 付款方式 rail for the
// merchant transfer 付款方式 filter). Verifies the column + partial index are
// created, a transfer attempt can carry the rail, a legacy row stays NULL, and
// re-applying the migration changes nothing.
func TestTransferMethod138MigrationAddsNullableMethodAndIsReentrant(t *testing.T) {
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
	schema := fmt.Sprintf("transfer_method138_%d", time.Now().UnixNano())
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
	// Apply everything strictly before 000138 as the installer records it,
	// then let Migrate apply the rest (including mine).
	for _, entry := range entries {
		if entry.Name() >= "000138_" {
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
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("migrate through 138", err)
	}
	if err = VerifySchema(ctx, p); err != nil {
		t.Fatal(err)
	}

	// Column exists and is nullable.
	var nullable string
	if err = p.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns WHERE table_schema=$1 AND table_name='provider_transfer_attempts' AND column_name='method'`, schema).Scan(&nullable); err != nil {
		t.Fatal("method column missing:", err)
	}
	if nullable != "YES" {
		t.Fatalf("method must be nullable, got %q", nullable)
	}
	// Partial index exists.
	var idx int
	if err = p.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname=$1 AND indexname='provider_transfer_attempts_method'`, schema).Scan(&idx); err != nil || idx != 1 {
		t.Fatalf("partial index provider_transfer_attempts_method expected once: %d %v", idx, err)
	}

	// Seed a merchant + transfer and one attempt carrying a rail; a second
	// attempt with NULL method (legacy write path) is accepted.
	if _, err = p.Exec(ctx, `INSERT INTO merchants(id,name,api_key,enabled)VALUES(5138,'method-mig','k-method-138'::bytea,true)`); err != nil {
		t.Fatal(err)
	}
	for i, method := range []any{"alipay", nil} {
		var tid int64
		if err = p.QueryRow(ctx, `INSERT INTO transfers(merchant_id,transfer_no,amount)VALUES(5138,$1,100)RETURNING id`, fmt.Sprintf("TM%d", i)).Scan(&tid); err != nil {
			t.Fatal(err)
		}
		if _, err = p.Exec(ctx, `INSERT INTO provider_transfer_attempts(transfer_id,channel_id,recipient_cipher,method)VALUES($1,7,'\x00'::bytea,$2)`, tid, method); err != nil {
			t.Fatalf("attempt with method=%v rejected: %v", method, err)
		}
	}
	var withRail, legacyNull int
	if err = p.QueryRow(ctx, `SELECT count(*)FROM provider_transfer_attempts WHERE method='alipay'`).Scan(&withRail); err != nil || withRail != 1 {
		t.Fatalf("rail row count=%d err=%v", withRail, err)
	}
	if err = p.QueryRow(ctx, `SELECT count(*)FROM provider_transfer_attempts WHERE method IS NULL`).Scan(&legacyNull); err != nil || legacyNull != 1 {
		t.Fatalf("legacy NULL row count=%d err=%v", legacyNull, err)
	}

	// Reentrant.
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("reentry", err)
	}
	var count int
	if err = p.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name='000138_transfer_method.up.sql'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration must register exactly once: %d %v", count, err)
	}
}
