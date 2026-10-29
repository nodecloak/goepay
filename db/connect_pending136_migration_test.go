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

// TestConnectPending136MigrationForwardOnly applies through 000136 and asserts
// the pending-connect store: identity immutable, single-use consume, no delete,
// provider CHECK, and reentrancy.
func TestConnectPending136MigrationForwardOnly(t *testing.T) {
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
	schema := fmt.Sprintf("pending136_%d", time.Now().UnixNano())
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
	isCheck := func(err error) bool {
		var pg *pgconn.PgError
		return errors.As(err, &pg) && (pg.Code == "P0001" || pg.Code == "23514")
	}
	h := func(s string) []byte { x := sha256.Sum256([]byte(s)); return x[:] }

	st := h("pending-1")
	if _, err = p.Exec(ctx, `INSERT INTO connect_pending136(state_hash,provider,social_uid,browser_hash)VALUES($1,'qq','QQ-OPENID',$2)`, st, h("b")); err != nil {
		t.Fatal("insert", err)
	}
	if _, err = p.Exec(ctx, `INSERT INTO connect_pending136(state_hash,provider,social_uid,browser_hash)VALUES($1,'weibo','x',$2)`, h("bad"), h("b")); !isCheck(err) {
		t.Fatal("unknown provider refused", err)
	}
	// single-use.
	if _, err = p.Exec(ctx, `UPDATE connect_pending136 SET consumed_at=clock_timestamp()WHERE state_hash=$1`, st); err != nil {
		t.Fatal("first consume", err)
	}
	if _, err = p.Exec(ctx, `UPDATE connect_pending136 SET consumed_at=clock_timestamp()WHERE state_hash=$1`, st); !isCheck(err) {
		t.Fatal("second consume must be refused", err)
	}
	// identity immutable.
	if _, err = p.Exec(ctx, `UPDATE connect_pending136 SET social_uid='other' WHERE state_hash=$1`, st); !isCheck(err) {
		t.Fatal("social_uid immutable", err)
	}
	// no delete.
	if _, err = p.Exec(ctx, `DELETE FROM connect_pending136 WHERE state_hash=$1`, st); !isCheck(err) {
		t.Fatal("delete refused", err)
	}
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("reentry", err)
	}
	var reg int
	raw, _ := files.ReadFile("migrations/000139_connect_pending136.up.sql")
	if err = p.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name='000139_connect_pending136.up.sql' AND sha256=$1`, fmt.Sprintf("%x", sha256.Sum256(raw))).Scan(&reg); err != nil || reg != 1 {
		t.Fatal("register once", reg, err)
	}
}
