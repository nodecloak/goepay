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

// TestConnectLoginStates135MigrationForwardOnly applies every migration through
// 000135 and asserts the quick-login state store: a bind state requires a
// merchant and a login state forbids one; the row is single-use (consumed_at
// writes once, never twice) and immutable otherwise; no delete; a second
// Migrate is a no-op.
func TestConnectLoginStates135MigrationForwardOnly(t *testing.T) {
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
	schema := fmt.Sprintf("connect135_%d", time.Now().UnixNano())
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
		return errors.As(err, &pg) && (pg.Code == "P0001" || pg.Code == "23514" || pg.Code == "23503")
	}
	h := func(s string) []byte { x := sha256.Sum256([]byte(s)); return x[:] }

	// A merchant to own bind states.
	if _, err = p.Exec(ctx, `INSERT INTO merchants(id,name,api_key)VALUES(5001,'connect135-merchant','synthetic135-key-never-live')`); err != nil {
		t.Fatal("seed merchant", err)
	}

	// login state: no merchant allowed.
	login := h("state-login-1")
	if _, err = p.Exec(ctx, `INSERT INTO connect_login_states135(state_hash,provider,purpose,browser_hash)VALUES($1,'qq','login',$2)`, login, h("browser-1")); err != nil {
		t.Fatal("insert login state", err)
	}
	if _, err = p.Exec(ctx, `INSERT INTO connect_login_states135(state_hash,provider,purpose,browser_hash,merchant_id)VALUES($1,'qq','login',$2,5001)`, h("bad"), h("b")); !isCheck(err) {
		t.Fatal("login state with a merchant must be refused", err)
	}
	// bind state: merchant required.
	bind := h("state-bind-1")
	if _, err = p.Exec(ctx, `INSERT INTO connect_login_states135(state_hash,provider,purpose,browser_hash,merchant_id)VALUES($1,'qq','bind',$2,5001)`, bind, h("browser-2")); err != nil {
		t.Fatal("insert bind state", err)
	}
	if _, err = p.Exec(ctx, `INSERT INTO connect_login_states135(state_hash,provider,purpose,browser_hash)VALUES($1,'qq','bind',$2)`, h("bad2"), h("b")); !isCheck(err) {
		t.Fatal("bind state without a merchant must be refused", err)
	}
	// unknown provider / purpose refused.
	if _, err = p.Exec(ctx, `INSERT INTO connect_login_states135(state_hash,provider,purpose,browser_hash)VALUES($1,'weibo','login',$2)`, h("bad3"), h("b")); !isCheck(err) {
		t.Fatal("unknown provider refused", err)
	}

	// single-use: consume once is allowed, twice is refused.
	if _, err = p.Exec(ctx, `UPDATE connect_login_states135 SET consumed_at=clock_timestamp()WHERE state_hash=$1`, login); err != nil {
		t.Fatal("first consume rejected", err)
	}
	if _, err = p.Exec(ctx, `UPDATE connect_login_states135 SET consumed_at=clock_timestamp()WHERE state_hash=$1`, login); !isCheck(err) {
		t.Fatal("second consume must be refused", err)
	}
	// identity immutable.
	if _, err = p.Exec(ctx, `UPDATE connect_login_states135 SET provider='wx' WHERE state_hash=$1`, bind); !isCheck(err) {
		t.Fatal("provider must be immutable", err)
	}
	// no delete.
	if _, err = p.Exec(ctx, `DELETE FROM connect_login_states135 WHERE state_hash=$1`, bind); !isCheck(err) {
		t.Fatal("delete must be refused", err)
	}

	// reentrancy.
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("reentry", err)
	}
	var reg int
	raw, _ := files.ReadFile("migrations/000135_connect_login_states135.up.sql")
	if err = p.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name='000135_connect_login_states135.up.sql' AND sha256=$1`, fmt.Sprintf("%x", sha256.Sum256(raw))).Scan(&reg); err != nil || reg != 1 {
		t.Fatal("migration must register exactly once", reg, err)
	}
}
