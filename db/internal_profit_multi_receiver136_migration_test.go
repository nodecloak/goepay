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

// 000136: 站内分账 multi-receiver allocation support (a4 N3). Verifies the
// additive recipient_id column + partial unique index are created, that the
// migration is reentrant, and that the partial unique index enforces per-receiver
// uniqueness only for non-NULL recipient_id (legacy single-receiver rows, whose
// recipient_id stays NULL, keep the original UNIQUE(order_id,rule_id)).
func TestInternalProfitMultiReceiver136MigrationAddsRecipientColumnAndPartialUnique(t *testing.T) {
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
	schema := fmt.Sprintf("profit_multi136_%d", time.Now().UnixNano())
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
		if entry.Name() >= "000136_" {
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
		t.Fatal("migrate through 136", err)
	}
	if err = VerifySchema(ctx, p); err != nil {
		t.Fatal(err)
	}

	// recipient_id column exists and references merchants.
	var dataType string
	if err = p.QueryRow(ctx, `SELECT data_type FROM information_schema.columns WHERE table_schema=$1 AND table_name='order_profit_allocations' AND column_name='recipient_id'`, schema).Scan(&dataType); err != nil {
		t.Fatal("recipient_id column missing", err)
	}
	if dataType != "bigint" {
		t.Fatalf("recipient_id type=%s want bigint", dataType)
	}
	var fkExists bool
	if err = p.QueryRow(ctx, `SELECT EXISTS(
	 SELECT 1 FROM information_schema.key_column_usage k
	 JOIN information_schema.table_constraints c ON c.constraint_name=k.constraint_name AND c.constraint_schema=k.constraint_schema
	 WHERE k.table_schema=$1 AND k.table_name='order_profit_allocations' AND k.column_name='recipient_id' AND c.constraint_type='FOREIGN KEY')`, schema).Scan(&fkExists); err != nil {
		t.Fatal(err)
	}
	if !fkExists {
		t.Fatal("recipient_id FK to merchants missing")
	}

	// Partial unique index present.
	var idxDef string
	if err = p.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname=$1 AND indexname='order_profit_allocations_recipient_uniq'`, schema).Scan(&idxDef); err != nil {
		t.Fatal("partial unique index missing", err)
	}
	if !contains(idxDef, "recipient_id IS NOT NULL") || !contains(idxDef, "UNIQUE") {
		t.Fatalf("index not a partial unique on non-NULL recipient: %s", idxDef)
	}

	// Reentrant.
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("reentry", err)
	}
	var count int
	if err = p.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name='000136_internal_profit_multi_receiver.up.sql'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration must register exactly once: %d %v", count, err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
