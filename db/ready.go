package db

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VerifySchema prevents a new API binary from reporting ready against an older
// schema. Migrations run explicitly as a deployment step, never on a web request.
func VerifySchema(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := files.ReadDir("migrations")
	if err != nil {
		return err
	}
	expected := make(map[string]string, len(entries))
	for _, entry := range entries {
		raw, e := files.ReadFile("migrations/" + entry.Name())
		if e != nil {
			return e
		}
		expected[entry.Name()] = fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	rows, err := pool.Query(ctx, "SELECT name,sha256 FROM schema_migrations")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, digest string
		if err = rows.Scan(&name, &digest); err != nil {
			return err
		}
		if want, ok := expected[name]; ok {
			if digest != want {
				return fmt.Errorf("migration checksum mismatch: %s", name)
			}
			delete(expected, name)
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(expected) > 0 {
		return fmt.Errorf("database migrations pending")
	}
	return nil
}
