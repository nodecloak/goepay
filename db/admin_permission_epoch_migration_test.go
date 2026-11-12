package db

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdminPermissionEpochUpgradePreservesExistingAuthorityUntilChanged(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("actual PostgreSQL required")
	}
	ctx := t.Context()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("permission_epoch_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	entries, err := files.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	// Recreate a deployed installation through the login-name epoch migration.
	for _, entry := range entries {
		if entry.Name() >= "000196_" {
			continue
		}
		raw, err := files.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, string(raw)); err != nil {
			t.Fatal(entry.Name(), err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO schema_migrations(name,sha256) VALUES($1,$2)`, entry.Name(), fmt.Sprintf("%x", sha256.Sum256(raw))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class,permissions) VALUES('deployed-operator','admin',8,'Deployed','synthetic-hash','operator','["OrderList"]'); INSERT INTO console_sessions(token_hash,username,role,subject_id,csrf,expires_at,auth_version) VALUES(decode(repeat('11',32),'hex'),'deployed-operator','admin',8,repeat('c',43),clock_timestamp()+interval '1 day',1)`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var result string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_object('users',(SELECT jsonb_agg(to_jsonb(u)) FROM console_users u),'sessions',(SELECT jsonb_agg(to_jsonb(s)-'last_seen_at') FROM console_sessions s))::text`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal("upgrade from deployed schema", err)
	}
	if snapshot() != before {
		t.Fatal("upgrade changed existing account or session facts")
	}
	if err = VerifySchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		update string
		want   int64
	}{
		{`SET permissions='["OrderList"]'`, 1},
		{`SET display_name='Renamed'`, 1},
		{`SET permissions='[]'`, 2},
		{`SET permissions=NULL`, 3},
		{`SET permissions=NULL`, 3},
	} {
		var version int64
		if err = pool.QueryRow(ctx, "UPDATE console_users "+tc.update+" WHERE username='deployed-operator' RETURNING auth_version").Scan(&version); err != nil || version != tc.want {
			t.Errorf("%s: auth_version=%d want=%d err=%v", tc.update, version, tc.want, err)
		}
	}
	var valid int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM console_sessions s JOIN console_users u USING(username,role) WHERE s.auth_version=u.auth_version AND u.enabled`).Scan(&valid); err != nil || valid != 0 {
		t.Errorf("pre-change session retained authority: valid=%d err=%v", valid, err)
	}
	final := snapshot()
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal("reapply must preserve old migration checksums", err)
	}
	if snapshot() != final {
		t.Fatal("migration reentry changed account or session facts")
	}
}
