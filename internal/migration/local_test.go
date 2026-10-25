package migration

import "testing"

func TestLocalDatabaseOnly(t *testing.T) {
	for _, dsn := range []string{"postgres://user:pass@127.0.0.1:55432/db?sslmode=disable", "postgres://user:pass@[::1]:55432/db"} {
		if !LocalDSN(dsn) {
			t.Fatal("loopback rejected")
		}
	}
	for _, dsn := range []string{"postgres://user:pass@db.example/db", "postgres://127.0.0.1/db?host=production.example", "postgres://127.0.0.1/db?service=prod", "host=127.0.0.1 dbname=db", "postgres://localhost/db", "postgres://127.0.0.1.evil.invalid/db"} {
		if LocalDSN(dsn) {
			t.Fatal("nonlocal override accepted")
		}
	}
}
