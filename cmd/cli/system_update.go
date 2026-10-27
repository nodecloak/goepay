package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/systemupdate"
)

// Verification uses the actual database migration catalog, the linked current
// build, and deployment trust. It never commits an artifact or runs package code.
func verifyLocalUpdate183(ctx context.Context, pool *pgxpool.Pool, output io.Writer) error {
	service, e := systemupdate.FromEnvironment(os.Getenv)
	if e != nil {
		return e
	}
	rows, e := pool.Query(ctx, `SELECT name,sha256 FROM schema_migrations ORDER BY name`)
	if e != nil {
		return e
	}
	applied := map[string]string{}
	var schema int64
	for rows.Next() {
		var name, hash string
		if e = rows.Scan(&name, &hash); e != nil {
			rows.Close()
			return e
		}
		var n int64
		if _, e = fmt.Sscanf(name, "%d_", &n); e != nil {
			rows.Close()
			return e
		}
		if n > schema {
			schema = n
		}
		applied[name] = hash
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	build := systemupdate.RuntimeBuild(schema)
	view, e := service.Inspect(ctx, build)
	if e != nil {
		return e
	}
	if !view.CanPrepare {
		return fmt.Errorf("local update is not verifiable for the linked build: %s", view.CheckStatus)
	}
	if e = service.VerifyPackage(ctx, build, view.Fingerprint, applied); e != nil {
		return e
	}
	return json.NewEncoder(output).Encode(map[string]any{"verified": true, "prepared": false, "applied": false, "intent": view.Fingerprint, "target_version": view.LatestVersion, "target_source_sha256": view.LatestSource})
}
