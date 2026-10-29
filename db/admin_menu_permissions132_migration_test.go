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

// TestAdminMenuPermissions132Migration applies every migration through 000132 on
// a throwaway schema and asserts: console_users gains a nullable permissions
// jsonb column (NULL = unrestricted operator), the array-type CHECK rejects a
// non-array value, a JSON array is accepted, the login-audit created_at index
// exists, and a second Migrate is a no-op registering exactly once.
func TestAdminMenuPermissions132Migration(t *testing.T) {
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
	schema := fmt.Sprintf("adminperm132_%d", time.Now().UnixNano())
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

	if err = Migrate(ctx, p); err != nil {
		t.Fatal("migrate", err)
	}
	if err = VerifySchema(ctx, p); err != nil {
		t.Fatal(err)
	}

	hash := "$argon2id$v=19$m=65536,t=2,p=2$c2FsdHNhbHRzYWx0c2E$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g"
	// A pre-migration operator row carries NULL permissions = unrestricted.
	if _, err = p.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('legacy-op','admin',9001,'Legacy',$1,'operator')", hash); err != nil {
		t.Fatal("insert legacy operator", err)
	}
	var isNull bool
	if err = p.QueryRow(ctx, "SELECT permissions IS NULL FROM console_users WHERE username='legacy-op'").Scan(&isNull); err != nil || !isNull {
		t.Fatalf("legacy operator permissions must default NULL: null=%v err=%v", isNull, err)
	}

	isCheck := func(err error) bool {
		var pg *pgconn.PgError
		return errors.As(err, &pg) && pg.Code == "23514"
	}
	// A non-array value is rejected by the CHECK constraint.
	if _, err = p.Exec(ctx, "UPDATE console_users SET permissions='\"OrderList\"'::jsonb WHERE username='legacy-op'"); !isCheck(err) {
		t.Fatalf("non-array permissions must be refused, got %v", err)
	}
	if _, err = p.Exec(ctx, "UPDATE console_users SET permissions='{\"a\":1}'::jsonb WHERE username='legacy-op'"); !isCheck(err) {
		t.Fatalf("object permissions must be refused, got %v", err)
	}
	// A JSON array is accepted.
	if _, err = p.Exec(ctx, "UPDATE console_users SET permissions='[\"OrderList\",\"DailyStat\"]'::jsonb WHERE username='legacy-op'"); err != nil {
		t.Fatalf("array permissions must be accepted, got %v", err)
	}

	// The login-audit retention index exists.
	var idx int
	if err = p.QueryRow(ctx, "SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname='console_login_audit_created_at_idx'").Scan(&idx); err != nil || idx != 1 {
		t.Fatalf("login-audit created_at index missing: count=%d err=%v", idx, err)
	}

	// Login audit is append-only after 000132: UPDATE stays refused, DELETE is
	// permitted for age-bounded retention.
	var auditID int64
	if err = p.QueryRow(ctx, "INSERT INTO console_login_audit(username,role,ip,success) VALUES('a','admin','127.0.0.1',true) RETURNING id").Scan(&auditID); err != nil {
		t.Fatal("insert login audit", err)
	}
	if _, err = p.Exec(ctx, "UPDATE console_login_audit SET success=false WHERE id=$1", auditID); err == nil {
		t.Fatal("login audit UPDATE must still be refused")
	}
	if _, err = p.Exec(ctx, "DELETE FROM console_login_audit WHERE id=$1", auditID); err != nil {
		t.Fatalf("login audit DELETE must be permitted for retention: %v", err)
	}

	// Reentrancy: a second Migrate changes nothing and registers once.
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("reentry", err)
	}
	var reg int
	raw, _ := files.ReadFile("migrations/000132_admin_menu_permissions.up.sql")
	if err = p.QueryRow(ctx, "SELECT count(*) FROM schema_migrations WHERE name='000132_admin_menu_permissions.up.sql' AND sha256=$1", fmt.Sprintf("%x", sha256.Sum256(raw))).Scan(&reg); err != nil || reg != 1 {
		t.Fatal("migration must register exactly once with matching hash", reg, err)
	}
}
