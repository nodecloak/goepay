package db

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 000137: admin_balance_adjustments (后台加款/后台扣款 audit). Verifies the table
// is created, enforces its CHECKs + FK + per-merchant external_no uniqueness, and
// that applying it is reentrant on an existing install.
func TestAdminBalanceAdjustments137MigrationCreatesBoundedAuditTableAndIsReentrant(t *testing.T) {
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
	schema := fmt.Sprintf("admin_adjust137_%d", time.Now().UnixNano())
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
	// Apply everything strictly before 000137 the way the installer records it,
	// then let Migrate apply the rest (including mine).
	for _, entry := range entries {
		if entry.Name() >= "000137_" {
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
		t.Fatal("migrate through 137", err)
	}
	if err = VerifySchema(ctx, p); err != nil {
		t.Fatal(err)
	}

	// Seed a merchant the FK can reference.
	if _, err = p.Exec(ctx, `INSERT INTO merchants(id,name,api_key,enabled)VALUES(5137,'adjust-mig','k-adjust-137'::bytea,true)`); err != nil {
		t.Fatal(err)
	}

	mustInsert := func(dir, ext, reason string, amt int64) {
		t.Helper()
		if _, err := p.Exec(ctx, `INSERT INTO admin_balance_adjustments(merchant_id,direction,amount,reason,external_no,actor)VALUES(5137,$1,$2,$3,$4,'admin:1')`, dir, amt, reason, ext); err != nil {
			t.Fatalf("valid adjustment rejected (%s %s): %v", dir, ext, err)
		}
	}
	mustReject := func(dir, ext, reason string, amt int64, constraint string) {
		t.Helper()
		_, err := p.Exec(ctx, `INSERT INTO admin_balance_adjustments(merchant_id,direction,amount,reason,external_no,actor)VALUES(5137,$1,$2,$3,$4,'admin:1')`, dir, amt, reason, ext)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) {
			t.Fatalf("expected constraint error for (%s %s), got %v", dir, ext, err)
		}
	}

	mustInsert("credit", "ok-1", "补偿", 500)
	mustInsert("debit", "ok-2", "回收", 300)
	// CHECKs: bad direction, amount<=0, empty reason, empty external_no.
	mustReject("sideways", "bad-dir", "r", 1, "direction")
	mustReject("credit", "bad-amt", "r", 0, "amount")
	mustReject("credit", "bad-reason", "", 1, "reason")
	mustReject("credit", "", "r", 1, "external_no")
	// Unique (merchant_id, external_no).
	mustReject("credit", "ok-1", "dup", 1, "unique")
	// FK: unknown merchant.
	if _, err = p.Exec(ctx, `INSERT INTO admin_balance_adjustments(merchant_id,direction,amount,reason,external_no,actor)VALUES(999999,'credit',1,'r','fk-x','admin:1')`); err == nil {
		t.Fatal("FK to merchants not enforced")
	}

	// Reentrant: applying again changes nothing and keeps the two rows.
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("reentry", err)
	}
	var count int
	if err = p.QueryRow(ctx, `SELECT count(*)FROM admin_balance_adjustments`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("rows after reentry=%d err=%v", count, err)
	}
	if err = p.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name='000137_admin_balance_adjustments.up.sql'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration must register exactly once: %d %v", count, err)
	}
}
