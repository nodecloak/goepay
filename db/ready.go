package db

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// embeddedChecksums hashes the embedded migrations once per process. They are
// part of the binary, so the readiness probe (public and polled) only has to
// compare them with the database instead of re-hashing every file each time.
var embeddedChecksums = sync.OnceValues(func() (map[string]string, error) {
	entries, err := files.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	sums := make(map[string]string, len(entries))
	for _, entry := range entries {
		raw, e := files.ReadFile("migrations/" + entry.Name())
		if e != nil {
			return nil, e
		}
		sums[entry.Name()] = fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	return sums, nil
})

// VerifySchema prevents a new API binary from reporting ready against an older
// schema. Migrations run explicitly as a deployment step, never on a web request.
func VerifySchema(ctx context.Context, pool *pgxpool.Pool) error {
	sums, err := embeddedChecksums()
	if err != nil {
		return err
	}
	pending := make(map[string]bool, len(sums))
	for name := range sums {
		pending[name] = true
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
		if want, ok := sums[name]; ok {
			if digest != want {
				return fmt.Errorf("migration checksum mismatch: %s", name)
			}
			delete(pending, name)
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("database migrations pending")
	}
	return nil
}
