package db

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestEnrollmentPhones134MigrationForwardOnlyAndReentrant applies every
// migration through 000131 on a throwaway schema and asserts the phone-verified
// enrollment claim: a phone is one-to-one with a merchant (UNIQUE phone_hash +
// UNIQUE merchant_id), the row is immutable (append-only, mirroring
// enrollment_emails), and a second Migrate is a no-op registering the file once
// with a matching hash. It changes no existing row.
func TestEnrollmentPhones134MigrationForwardOnlyAndReentrant(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("actual PostgreSQL required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("phones134_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	if err = Migrate(ctx, p); err != nil {
		t.Fatal("migrate", err)
	}
	if err = VerifySchema(ctx, p); err != nil {
		t.Fatal(err)
	}

	isCheck := func(err error) bool {
		var pg *pgconn.PgError
		return errors.As(err, &pg) && (pg.Code == "P0001" || pg.Code == "23514" || pg.Code == "23502" || pg.Code == "23505" || pg.Code == "23503")
	}

	// Two merchants + console users to claim phones against.
	for _, m := range []struct {
		id   int64
		user string
	}{{900001, "phone-one"}, {900002, "phone-two"}} {
		if _, err = p.Exec(ctx, `INSERT INTO merchants(id,name,api_key,enabled)VALUES($1,$2,'k',true)`, m.id, m.user); err != nil {
			t.Fatal("merchant", err)
		}
		if _, err = p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash)VALUES($1,'merchant',$2,$1,'h')`, m.user, m.id); err != nil {
			t.Fatal("console user", err)
		}
	}
	h1 := sha256.Sum256([]byte("one"))
	h2 := sha256.Sum256([]byte("two"))
	if _, err = p.Exec(ctx, `INSERT INTO enrollment_phones134(phone,phone_hash,merchant_id,username)VALUES('13800000001',$1,900001,'phone-one')`, h1[:]); err != nil {
		t.Fatal("first phone claim", err)
	}
	// A second merchant claiming the SAME phone_hash is refused (already registered).
	if _, err = p.Exec(ctx, `INSERT INTO enrollment_phones134(phone,phone_hash,merchant_id,username)VALUES('13800000002',$1,900002,'phone-two')`, h1[:]); !isCheck(err) {
		t.Fatal("duplicate phone_hash must be refused", err)
	}
	// The same merchant claiming a second phone is refused (one-to-one).
	if _, err = p.Exec(ctx, `INSERT INTO enrollment_phones134(phone,phone_hash,merchant_id,username)VALUES('13800000003',$1,900001,'phone-one')`, h2[:]); !isCheck(err) {
		t.Fatal("duplicate merchant must be refused", err)
	}
	// A too-short phone is refused.
	if _, err = p.Exec(ctx, `INSERT INTO enrollment_phones134(phone,phone_hash,merchant_id,username)VALUES('12',$1,900002,'phone-two')`, h2[:]); !isCheck(err) {
		t.Fatal("short phone must be refused", err)
	}
	// Immutable: no UPDATE, no DELETE.
	if _, err = p.Exec(ctx, `UPDATE enrollment_phones134 SET phone='13900000000' WHERE merchant_id=900001`); !isCheck(err) {
		t.Fatal("phone claim must be immutable", err)
	}
	if _, err = p.Exec(ctx, `DELETE FROM enrollment_phones134 WHERE merchant_id=900001`); !isCheck(err) {
		t.Fatal("phone claim delete must be refused", err)
	}

	// Reentrancy.
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("reentry", err)
	}
	var reg int
	raw, _ := files.ReadFile("migrations/000131_enrollment_phones134.up.sql")
	if err = p.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name='000131_enrollment_phones134.up.sql' AND sha256=$1`, fmt.Sprintf("%x", sha256.Sum256(raw))).Scan(&reg); err != nil || reg != 1 {
		t.Fatal("migration must register exactly once with matching hash", reg, err)
	}
}
