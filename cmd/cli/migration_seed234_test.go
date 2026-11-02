package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/identity"
	"goepay/internal/migration"
	"os"
	"testing"
)

func TestMigration234AdminSeedRejectsAbsentPool(t *testing.T) {
	if _, e := seedMigrationAdmin234(context.Background(), nil, "synthetic-strong-password234"); e == nil {
		t.Fatal("absent database accepted")
	}
}

func migrationSeedPool234(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	root, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	var salt [12]byte
	rand.Read(salt[:])
	schema := "migrationseed234_" + hex.EncodeToString(salt[:])
	if _, e = root.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	c, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	c.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close(); root.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); root.Close() })
	if e = db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	return p
}

func TestMigration234AdminSeedCreatesOnlyAnOriginalAdministrator(t *testing.T) {
	p := migrationSeedPool234(t)
	ctx := context.Background()
	password := "synthetic-strong-password234"
	created, e := seedMigrationAdmin234(ctx, p, password)
	if e != nil || !created {
		t.Fatal("fresh initialization failed", e)
	}
	created, e = seedMigrationAdmin234(ctx, p, "another-synthetic-password234")
	if e != nil || created {
		t.Fatal("repeat initialization replaced account", e)
	}
	var hash string
	var merchants, journals int
	if e = p.QueryRow(ctx, `SELECT password_hash,(SELECT count(*) FROM merchants),(SELECT count(*) FROM journals) FROM console_users WHERE username='admin' AND role='admin'`).Scan(&hash, &merchants, &journals); e != nil || !identity.CheckPassword(hash, password) || merchants != 0 || journals != 0 {
		t.Fatal("credentials or empty target changed", e)
	}
	s := identity.NewPersistentSessions(p, false)
	u := identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super", AuthVersion: 1}
	token, _, e := s.Create(u)
	if e != nil {
		t.Fatal(e)
	}
	w, e := migration.NewWizard234(p, bytes.Repeat([]byte{2}, 32))
	if e != nil {
		t.Fatal(e)
	}
	caps, e := w.Capabilities(ctx, migration.WizardActor234{User: u, Token: token})
	if e != nil || !caps.TargetReady {
		t.Fatal("administrator seed made wizard unavailable", e)
	}
}

func TestMigration234AdminSeedRefusesConcurrentBusinessWriter(t *testing.T) {
	p := migrationSeedPool234(t)
	tx, e := p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(t.Context(), `INSERT INTO merchants(id,name,api_key,enabled) VALUES(777,'Concurrent local',decode('00','hex'),false)`); e != nil {
		t.Fatal(e)
	}
	created, e := seedMigrationAdmin234(t.Context(), p, "synthetic-password234")
	if e == nil || created {
		t.Fatal("initialized an admin while target business writer was active")
	}
	var admins int
	if e = p.QueryRow(t.Context(), `SELECT count(*) FROM console_users WHERE role='admin'`).Scan(&admins); e != nil || admins != 0 {
		t.Fatal("failed setup left an administrator", e)
	}
}
