package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestOriginalDefaultGroupMigrationHandlesLegacyNamesAndReservedIdentity(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	migration, e := files.ReadFile("migrations/000091_default_group.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range []struct {
		name, seed string
		wantError  bool
	}{{"name collision", `INSERT INTO backoffice_records(id,resource,body) VALUES(7,'groups','{"name":"默认用户组"}')`, false}, {"existing exact zero", `INSERT INTO backoffice_records(id,resource,body) VALUES(0,'groups','{"name":"保留历史默认","info":{"alipay":{"channel":"0","type":""}}}')`, false}, {"foreign reserved identity", `INSERT INTO backoffice_records(id,resource,body) VALUES(0,'channels','{"name":"legacy reserved row"}')`, true}} {
		t.Run(c.name, func(t *testing.T) {
			schema := fmt.Sprintf("default154_%d", time.Now().UnixNano())
			if _, e := admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
				t.Fatal(e)
			}
			defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
			cfg, e := pgxpool.ParseConfig(dsn)
			if e != nil {
				t.Fatal(e)
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = schema
			p, e := pgxpool.NewWithConfig(ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer p.Close()
			if _, e = p.Exec(ctx, `CREATE TABLE backoffice_records(id bigserial PRIMARY KEY,resource text NOT NULL,body jsonb NOT NULL); CREATE UNIQUE INDEX group_name ON backoffice_records(lower(body->>'name')) WHERE resource='groups';`+c.seed); e != nil {
				t.Fatal(e)
			}
			_, e = p.Exec(ctx, string(migration))
			if c.wantError {
				if e == nil {
					t.Fatal("migration stole another resource's zero identity")
				}
				var resource string
				if e = p.QueryRow(ctx, `SELECT resource FROM backoffice_records WHERE id=0`).Scan(&resource); e != nil || resource != "channels" {
					t.Fatal("collision modified legacy record", resource, e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			var name string
			if e = p.QueryRow(ctx, `SELECT body->>'name' FROM backoffice_records WHERE id=0 AND resource='groups'`).Scan(&name); e != nil || name == "" {
				t.Fatal("missing real default", name, e)
			}
			if c.name == "existing exact zero" && name != "保留历史默认" {
				t.Fatal("replaced configured legacy default", name)
			}
			if c.name == "name collision" {
				var old string
				if e = p.QueryRow(ctx, `SELECT body->>'name' FROM backoffice_records WHERE id=7`).Scan(&old); e != nil || old != "默认用户组" || name == old {
					t.Fatal("renamed legacy custom group", name, old, e)
				}
			}
			if _, e = p.Exec(ctx, `DELETE FROM backoffice_records WHERE id=0`); e == nil {
				t.Fatal("default zero not protected")
			}
		})
	}
}
