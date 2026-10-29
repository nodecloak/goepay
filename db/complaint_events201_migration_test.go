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

// TestComplaintEvents201MigrationForwardOnlyAndReentrant applies every migration
// through 000141 on a throwaway schema and asserts the complaint event tables:
// an event is UNIQUE per (complaint_id, local_state) so a re-sync at the same
// state cannot insert a duplicate, identity is immutable/append-only, and a
// second Migrate is a no-op registering the file once with a matching hash.
func TestComplaintEvents201MigrationForwardOnlyAndReentrant(t *testing.T) {
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
	schema := fmt.Sprintf("complaint201_%d", time.Now().UnixNano())
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

	// A merchant, a channel's complaint, and two orders to link.
	if _, err = p.Exec(ctx, `INSERT INTO merchants(id,name,api_key,enabled)VALUES(30301,'c','k',true)`); err != nil {
		t.Fatal(err)
	}
	var complaintID int64
	if err = p.QueryRow(ctx, `INSERT INTO complaints133(pay_type,third_id,channel_id,owner_id,local_state,provider_status)VALUES('wxpay','CMP-301',7,30301,'open','PENDING')RETURNING id`).Scan(&complaintID); err != nil {
		t.Fatal("seed complaint", err)
	}
	mac := make([]byte, 32)
	body := []byte("{}")
	// First event for (complaint, open).
	if _, err = p.Exec(ctx, `INSERT INTO merchant_complaint_events201(complaint_id,local_state,merchant_id,body,mac)VALUES($1,'open',30301,$2,$3)`, complaintID, body, mac); err != nil {
		t.Fatal("first event", err)
	}
	// A second event for the SAME (complaint, open) is refused.
	if _, err = p.Exec(ctx, `INSERT INTO merchant_complaint_events201(complaint_id,local_state,merchant_id,body,mac)VALUES($1,'open',30301,$2,$3)`, complaintID, body, mac); !isCheck(err) {
		t.Fatal("duplicate (complaint,state) must be refused", err)
	}
	// A different state is a new event.
	if _, err = p.Exec(ctx, `INSERT INTO merchant_complaint_events201(complaint_id,local_state,merchant_id,body,mac)VALUES($1,'processing',30301,$2,$3)`, complaintID, body, mac); err != nil {
		t.Fatal("forward-state event", err)
	}
	// An unknown state is refused.
	if _, err = p.Exec(ctx, `INSERT INTO merchant_complaint_events201(complaint_id,local_state,merchant_id,body,mac)VALUES($1,'archived',30301,$2,$3)`, complaintID, body, mac); !isCheck(err) {
		t.Fatal("unknown state must be refused", err)
	}
	// Immutable: no UPDATE, no DELETE.
	if _, err = p.Exec(ctx, `UPDATE merchant_complaint_events201 SET local_state='done' WHERE complaint_id=$1 AND local_state='open'`, complaintID); !isCheck(err) {
		t.Fatal("event must be immutable", err)
	}
	if _, err = p.Exec(ctx, `DELETE FROM merchant_complaint_events201 WHERE complaint_id=$1`, complaintID); !isCheck(err) {
		t.Fatal("event delete must be refused", err)
	}

	// Reentrancy.
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("reentry", err)
	}
	var reg int
	raw, _ := files.ReadFile("migrations/000141_merchant_complaint_events201.up.sql")
	if err = p.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name='000141_merchant_complaint_events201.up.sql' AND sha256=$1`, fmt.Sprintf("%x", sha256.Sum256(raw))).Scan(&reg); err != nil || reg != 1 {
		t.Fatal("migration must register exactly once with matching hash", reg, err)
	}
}
