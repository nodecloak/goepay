package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"sort"
)

//go:embed migrations/*.sql
var files embed.FS

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	tx, e := pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(74973231)"); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())"); e != nil {
		return e
	}
	entries, e := files.ReadDir("migrations")
	if e != nil {
		return e
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, f := range entries {
		content, e := files.ReadFile("migrations/" + f.Name())
		if e != nil {
			return e
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(content))
		var previous string
		e = tx.QueryRow(ctx, "SELECT sha256 FROM schema_migrations WHERE name=$1", f.Name()).Scan(&previous)
		if e == nil {
			if previous != digest {
				return fmt.Errorf("migration checksum mismatch: %s", f.Name())
			}
			continue
		}
		if _, e = tx.Exec(ctx, string(content)); e != nil {
			return fmt.Errorf("migration %s: %w", f.Name(), e)
		}
		if _, e = tx.Exec(ctx, "INSERT INTO schema_migrations(name,sha256) VALUES($1,$2)", f.Name(), digest); e != nil {
			return e
		}
	}
	_, e = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS console_users(username text NOT NULL,role text NOT NULL CHECK(role IN ('admin','merchant')),subject_id bigint NOT NULL,display_name text NOT NULL,password_hash text NOT NULL,enabled boolean NOT NULL DEFAULT true,PRIMARY KEY(username,role))`)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
