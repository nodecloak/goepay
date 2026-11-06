package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"os"
	"testing"
	"time"
)

func identityTestPool(t *testing.T) *pgxpool.Pool {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("database required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 8)
	rand.Read(nonce)
	schema := "session_" + hex.EncodeToString(nonce)
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	cfg, _ := pgxpool.ParseConfig(dsn)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	scoped, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { scoped.Close(); pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); pool.Close() })
	if err = db.Migrate(ctx, scoped); err != nil {
		t.Fatal(err)
	}
	return scoped
}
func TestPersistentSessionsShareRevocationAndPasswordVersion(t *testing.T) {
	ctx := context.Background()
	scoped := identityTestPool(t)
	if _, err := scoped.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('admin','admin',1,'Admin','testhash')"); err != nil {
		t.Fatal(err)
	}
	a, b := NewPersistentSessions(scoped, false), NewPersistentSessions(scoped, false)
	u := User{Username: "admin", Role: "admin", ID: 1, Name: "Admin", AuthVersion: 1}
	token, csrf, err := a.Create(u)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := b.Get(token, "admin")
	if !ok || got.CSRF != csrf {
		t.Fatal("session lost across instances")
	}
	var raw int
	if err = scoped.QueryRow(ctx, "SELECT count(*) FROM console_sessions WHERE encode(token_hash,'hex')=$1", token).Scan(&raw); err != nil || raw != 0 {
		t.Fatal("plaintext token stored")
	}
	if _, ok = b.Get(token, "merchant"); ok {
		t.Fatal("role confusion")
	}
	if _, err = scoped.Exec(ctx, "UPDATE console_users SET password_hash='replacement' WHERE username='admin'"); err != nil {
		t.Fatal(err)
	}
	if _, ok = b.Get(token, "admin"); ok {
		t.Fatal("old password session remains valid")
	}
	u.AuthVersion = 2
	token, _, err = a.Create(u)
	if err != nil {
		t.Fatal(err)
	}
	b.RevokeUser("admin", "admin")
	if _, ok = a.Get(token, "admin"); ok {
		t.Fatal("revocation lost acrossinstances")
	}
}

func TestPersistentSessionsExpireWhenIdle(t *testing.T) {
	ctx := context.Background()
	scoped := identityTestPool(t)
	if _, err := scoped.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('admin','admin',1,'Admin','testhash')"); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	s := NewPersistentSessions(scoped, false)
	s.now = func() time.Time { return clock }
	s.AdminIdle = 30 * time.Minute
	u := User{Username: "admin", Role: "admin", ID: 1, Name: "Admin", AuthVersion: 1}
	token, _, err := s.Create(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []time.Duration{20 * time.Minute, 25 * time.Minute, 29 * time.Minute} {
		clock = clock.Add(step)
		if _, ok := s.Get(token, "admin"); !ok {
			t.Fatalf("session in use ended after %s of inactivity", step)
		}
	}
	clock = clock.Add(31 * time.Minute)
	if _, ok := s.Get(token, "admin"); ok {
		t.Fatal("idle administrator session still accepted")
	}
	var remaining int
	if err = scoped.QueryRow(ctx, "SELECT count(*) FROM console_sessions").Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("idle session was not revoked", remaining, err)
	}
	// Without an idle limit only the 12-hour absolute expiry applies.
	s.AdminIdle = 0
	token, _, err = s.Create(u)
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(11 * time.Hour)
	if _, ok := s.Get(token, "admin"); !ok {
		t.Fatal("session without idle limit ended early")
	}
	clock = clock.Add(2 * time.Hour)
	if _, ok := s.Get(token, "admin"); ok {
		t.Fatal("absolute expiry not enforced")
	}
}
