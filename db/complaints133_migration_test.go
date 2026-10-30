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

// TestComplaints133MigrationCreatesForwardOnlyStoreAndIsReentrant applies every
// migration through 000133 on a throwaway schema and asserts: the complaint
// store + task tables exist; provider identity, order linkage and amount are
// immutable; local_state and last_seen_at cannot move backward; (pay_type,
// third_id) upsert is unique; and a second Migrate is a no-op.
func TestComplaints133MigrationCreatesForwardOnlyStoreAndIsReentrant(t *testing.T) {
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
	schema := fmt.Sprintf("complaints133_%d", time.Now().UnixNano())
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

	// A merchant to own the complaint.
	var owner int64
	if err = p.QueryRow(ctx, `INSERT INTO merchants(id)VALUES(7001)RETURNING id`).Scan(&owner); err != nil {
		// merchants may require more columns; fall back to NULL owner.
		owner = 0
	}
	ownerArg := any(nil)
	if owner != 0 {
		ownerArg = owner
	}

	var id int64
	if err = p.QueryRow(ctx, `INSERT INTO complaints133(pay_type,third_id,channel_id,owner_id,trade_nos,category,provider_status,amount_cents)
		VALUES('wxpay','200000',5,$1,ARRAY['T100'],'PENDING','PENDING',1200)RETURNING id`, ownerArg).Scan(&id); err != nil {
		t.Fatal("insert complaint", err)
	}

	// Upsert on re-sync: same (pay_type, third_id) advances state, not a dup row.
	if _, err = p.Exec(ctx, `INSERT INTO complaints133(pay_type,third_id,channel_id,provider_status)
		VALUES('wxpay','200000',5,'PROCESSING')
		ON CONFLICT(pay_type,third_id)DO UPDATE SET provider_status=EXCLUDED.provider_status,local_state='processing',last_seen_at=now()`); err != nil {
		t.Fatal("upsert re-sync", err)
	}
	var count int
	if err = p.QueryRow(ctx, `SELECT count(*)FROM complaints133 WHERE pay_type='wxpay' AND third_id='200000'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("upsert must not duplicate", count, err)
	}

	isCheck := func(err error) bool {
		var pg *pgconn.PgError
		return errors.As(err, &pg) && (pg.Code == "P0001" || pg.Code == "23514")
	}

	// Identity immutable.
	if _, err = p.Exec(ctx, `UPDATE complaints133 SET third_id='999' WHERE id=$1`, id); !isCheck(err) {
		t.Fatal("third_id must be immutable", err)
	}
	// Amount immutable once known.
	if _, err = p.Exec(ctx, `UPDATE complaints133 SET amount_cents=1 WHERE id=$1`, id); !isCheck(err) {
		t.Fatal("amount must be immutable once known", err)
	}
	// Order linkage immutable.
	if _, err = p.Exec(ctx, `UPDATE complaints133 SET trade_nos=ARRAY['OTHER'] WHERE id=$1`, id); !isCheck(err) {
		t.Fatal("trade_nos must be immutable", err)
	}
	// local_state cannot move backward (currently 'processing').
	if _, err = p.Exec(ctx, `UPDATE complaints133 SET local_state='open' WHERE id=$1`, id); !isCheck(err) {
		t.Fatal("local_state must not move backward", err)
	}
	// Forward is allowed.
	if _, err = p.Exec(ctx, `UPDATE complaints133 SET local_state='done' WHERE id=$1`, id); err != nil {
		t.Fatal("forward local_state rejected", err)
	}
	// Delete is refused.
	if _, err = p.Exec(ctx, `DELETE FROM complaints133 WHERE id=$1`, id); !isCheck(err) {
		t.Fatal("delete must be refused", err)
	}

	// Task table basics.
	if _, err = p.Exec(ctx, `INSERT INTO complaint_tasks133(channel_id,num,frequency)VALUES(5,20,6)`); err != nil {
		t.Fatal("task insert", err)
	}
	// Migration 000175 widened num to epay_pro's 1..200; 201 is out of range.
	if _, err = p.Exec(ctx, `INSERT INTO complaint_tasks133(channel_id,num)VALUES(6,201)`); !isCheck(err) {
		t.Fatal("num out of range must be refused", err)
	}

	// Reentrancy: a second Migrate changes nothing and registers once.
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("reentry", err)
	}
	var reg int
	raw, _ := files.ReadFile("migrations/000133_complaints133.up.sql")
	if err = p.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name='000133_complaints133.up.sql' AND sha256=$1`, fmt.Sprintf("%x", sha256.Sum256(raw))).Scan(&reg); err != nil || reg != 1 {
		t.Fatal("migration must register exactly once with matching hash", reg, err)
	}
}
