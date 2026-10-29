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

// TestSMSVerification134MigrationForwardOnlyAndReentrant applies every
// migration through 000134 on a throwaway schema and asserts the W7 SMS store:
// a verification code's identity/issuance is immutable while status and
// errcount move only forward and it is never deleted; the gateway head refuses
// status=1 without an active provider; a sms test request settles exactly once;
// and a second Migrate is a no-op registering the file once with a matching
// hash.
func TestSMSVerification134MigrationForwardOnlyAndReentrant(t *testing.T) {
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
	schema := fmt.Sprintf("sms134_%d", time.Now().UnixNano())
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
		return errors.As(err, &pg) && (pg.Code == "P0001" || pg.Code == "23514" || pg.Code == "23502")
	}

	// --- verification_codes134 --------------------------------------------
	var codeID int64
	if err = p.QueryRow(ctx, `INSERT INTO verification_codes134(uid,scene,type,recipient,code,ip)
		VALUES(0,'reg',1,'13800000000','123456','203.0.113.9')RETURNING id`).Scan(&codeID); err != nil {
		t.Fatal("insert code", err)
	}
	// A bad scene/code shape is refused.
	if _, err = p.Exec(ctx, `INSERT INTO verification_codes134(scene,type,recipient,code)VALUES('nope',1,'x','123456')`); !isCheck(err) {
		t.Fatal("bad scene must be refused", err)
	}
	if _, err = p.Exec(ctx, `INSERT INTO verification_codes134(scene,type,recipient,code)VALUES('reg',1,'x','abcd')`); !isCheck(err) {
		t.Fatal("non-numeric code must be refused", err)
	}
	// Identity/issuance immutable.
	if _, err = p.Exec(ctx, `UPDATE verification_codes134 SET code='999999' WHERE id=$1`, codeID); !isCheck(err) {
		t.Fatal("code must be immutable", err)
	}
	if _, err = p.Exec(ctx, `UPDATE verification_codes134 SET recipient='13900000000' WHERE id=$1`, codeID); !isCheck(err) {
		t.Fatal("recipient must be immutable", err)
	}
	// errcount forward is allowed; backward refused.
	if _, err = p.Exec(ctx, `UPDATE verification_codes134 SET errcount=2 WHERE id=$1`, codeID); err != nil {
		t.Fatal("errcount forward rejected", err)
	}
	if _, err = p.Exec(ctx, `UPDATE verification_codes134 SET errcount=1 WHERE id=$1`, codeID); !isCheck(err) {
		t.Fatal("errcount must not move backward", err)
	}
	// status forward (consume) allowed; reopen refused.
	if _, err = p.Exec(ctx, `UPDATE verification_codes134 SET status=1 WHERE id=$1`, codeID); err != nil {
		t.Fatal("consume rejected", err)
	}
	if _, err = p.Exec(ctx, `UPDATE verification_codes134 SET status=0 WHERE id=$1`, codeID); !isCheck(err) {
		t.Fatal("status must not reopen", err)
	}
	// Delete refused.
	if _, err = p.Exec(ctx, `DELETE FROM verification_codes134 WHERE id=$1`, codeID); !isCheck(err) {
		t.Fatal("delete must be refused", err)
	}

	// --- sms gateway head + providers -------------------------------------
	seal := make([]byte, 64)
	mac := make([]byte, 32)
	if _, err = p.Exec(ctx, `INSERT INTO sms_providers134(provider,sealed,mac,configured)VALUES('aliyun',$1,$2,true)`, seal, mac); err != nil {
		t.Fatal("insert provider", err)
	}
	// An unknown provider is refused.
	if _, err = p.Exec(ctx, `INSERT INTO sms_providers134(provider,sealed,mac)VALUES('twilio',$1,$2)`, seal, mac); !isCheck(err) {
		t.Fatal("unknown provider must be refused", err)
	}
	// A short mac is refused.
	if _, err = p.Exec(ctx, `INSERT INTO sms_providers134(provider,sealed,mac)VALUES('qcloud',$1,$2)`, seal, make([]byte, 16)); !isCheck(err) {
		t.Fatal("short mac must be refused", err)
	}
	// Enabling without an active provider is refused by the head CHECK.
	if _, err = p.Exec(ctx, `UPDATE sms_gateway_head134 SET status=1 WHERE singleton`); !isCheck(err) {
		t.Fatal("enable without active provider must be refused", err)
	}
	// Enabling with an active provider is allowed.
	if _, err = p.Exec(ctx, `UPDATE sms_gateway_head134 SET active='aliyun',status=1 WHERE singleton`); err != nil {
		t.Fatal("enable with active provider rejected", err)
	}

	// --- sms_test_requests134 settle-once ---------------------------------
	rid := "11111111-1111-4111-8111-111111111111"
	rh := make([]byte, 32)
	if _, err = p.Exec(ctx, `INSERT INTO sms_test_requests134(request_id,provider,actor_id,recipient_hash)VALUES($1,'aliyun',7,$2)`, rid, rh); err != nil {
		t.Fatal("insert test request", err)
	}
	if _, err = p.Exec(ctx, `UPDATE sms_test_requests134 SET status='accepted',settled_at=now() WHERE request_id=$1`, rid); err != nil {
		t.Fatal("first settle rejected", err)
	}
	// A second settle is refused (outcome written once).
	if _, err = p.Exec(ctx, `UPDATE sms_test_requests134 SET status='failed' WHERE request_id=$1`, rid); !isCheck(err) {
		t.Fatal("second settle must be refused", err)
	}
	// Identity immutable.
	if _, err = p.Exec(ctx, `UPDATE sms_test_requests134 SET actor_id=9 WHERE request_id=$1`, rid); !isCheck(err) {
		t.Fatal("actor_id must be immutable", err)
	}
	// Delete refused.
	if _, err = p.Exec(ctx, `DELETE FROM sms_test_requests134 WHERE request_id=$1`, rid); !isCheck(err) {
		t.Fatal("test request delete must be refused", err)
	}

	// --- reentrancy -------------------------------------------------------
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("reentry", err)
	}
	var reg int
	raw, _ := files.ReadFile("migrations/000134_sms_verification134.up.sql")
	if err = p.QueryRow(ctx, `SELECT count(*)FROM schema_migrations WHERE name='000134_sms_verification134.up.sql' AND sha256=$1`, fmt.Sprintf("%x", sha256.Sum256(raw))).Scan(&reg); err != nil || reg != 1 {
		t.Fatal("migration must register exactly once with matching hash", reg, err)
	}
}
