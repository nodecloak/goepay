package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/migration"
	"io"
	"os"
	"time"
)

func run() int {
	input := flag.String("input", "", "sanitized snapshot JSON path")
	apply := flag.Bool("apply", false, "apply atomically to an isolated local PostgreSQL database")
	dsn := flag.String("database-url", "", "loopback PostgreSQL URL (or GOEPAY_IMPORT_DATABASE_URL)")
	flag.Parse()
	if *input == "" {
		fmt.Fprintln(os.Stderr, "--input is required")
		return 2
	}
	file, e := os.Open(*input)
	if e != nil {
		fmt.Fprintln(os.Stderr, "cannot open snapshot")
		return 1
	}
	defer file.Close()
	raw, e := io.ReadAll(io.LimitReader(file, 64<<20+1))
	if e != nil {
		fmt.Fprintln(os.Stderr, "cannot read snapshot")
		return 1
	}
	_, report, e := migration.Validate(raw)
	if e != nil {
		json.NewEncoder(os.Stdout).Encode(report)
		return 1
	}
	if !*apply {
		json.NewEncoder(os.Stdout).Encode(report)
		return 0
	}
	if *dsn == "" {
		*dsn = os.Getenv("GOEPAY_IMPORT_DATABASE_URL")
	}
	if !migration.LocalDSN(*dsn) {
		fmt.Fprintln(os.Stderr, "apply requires an explicit loopback PostgreSQL URL; remote targets and host overrides are rejected")
		return 2
	}
	master, e := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	if e != nil || len(master) != 32 {
		fmt.Fprintln(os.Stderr, "GOEPAY_MASTER_KEY must contain 32 hex-encoded bytes")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool, e := pgxpool.New(ctx, *dsn)
	if e != nil {
		fmt.Fprintln(os.Stderr, "cannot initialize local database")
		return 1
	}
	defer pool.Close()
	if e := db.Migrate(ctx, pool); e != nil {
		fmt.Fprintln(os.Stderr, "local database migration failed")
		return 1
	}
	report, e = migration.Apply(ctx, pool, master, raw)
	json.NewEncoder(os.Stdout).Encode(report)
	if e != nil {
		fmt.Fprintln(os.Stderr, "snapshot import rejected; target must be isolated, validated and free of financial history")
		return 1
	}
	return 0
}
func main() { os.Exit(run()) }
