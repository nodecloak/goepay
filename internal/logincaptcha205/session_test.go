package logincaptcha205

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/captcha203"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func sessionFixture205(t *testing.T, max int32) (*Service, *pgxpool.Pool, *identity.Sessions, identity.User) {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := t.Context()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	entropy := make([]byte, 8)
	if _, e = rand.Read(entropy); e != nil {
		t.Fatal(e)
	}
	schema := "login205_" + hex.EncodeToString(entropy)
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	if max > 0 {
		cfg.MaxConns = max
	}
	scoped, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { scoped.Close(); pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	if e = db.Migrate(ctx, scoped); e != nil {
		t.Fatal(e)
	}
	store, e := payment.NewWithKey(scoped, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	if e = store.CreateMerchant(ctx, payment.Merchant{ID: 1, Name: "Synthetic login205", APIKey: "synthetic-only205"}); e != nil {
		t.Fatal(e)
	}
	if _, e = scoped.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash)VALUES('login205','merchant',1,'Synthetic login205','unused-synthetic205')`); e != nil {
		t.Fatal(e)
	}
	s, e := New(scoped, make([]byte, 32), "https://console.example.invalid", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	return s, scoped, identity.NewPersistentSessions(scoped, false), identity.User{Username: "login205", ID: 1, Name: "Synthetic login205", Role: "merchant", AuthVersion: 1}
}
func disabledGrant205(t *testing.T, s *Service) Grant {
	t.Helper()
	g, e := s.Consume(t.Context(), httptest.NewRequest("POST", "/console/v1/merchant/login", nil), "login205", captcha203.Proof{})
	if e != nil {
		t.Fatal(e)
	}
	return g
}

func TestLoginCaptcha205DisabledGrantCannotSurviveConcurrentEnablement(t *testing.T) {
	s, p, sessions, u := sessionFixture205(t, 0)
	g := disabledGrant205(t, s)
	if _, e := p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"captcha_open_login","value":"1"}')`); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.IssueSession(t.Context(), g, u, sessions); !errors.Is(e, ErrConflict) {
		t.Fatal("disabled original grant survived enabled configuration", e)
	}
	var count int
	if e := p.QueryRow(t.Context(), `SELECT count(*) FROM console_sessions`).Scan(&count); e != nil || count != 0 {
		t.Fatal("changed policy created a session")
	}
}
func TestLoginCaptcha205SessionInsertionExpiresAtOriginalGrantAfterLastWrite(t *testing.T) {
	s, p, sessions, u := sessionFixture205(t, 0)
	g := disabledGrant205(t, s)
	// A short original in-process lease isolates the final-clock boundary without
	// waiting two minutes or changing any persisted challenge/proof.
	if _, e := p.Exec(t.Context(), `CREATE SEQUENCE login205_insert_seen;
CREATE FUNCTION login205_delay() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('login205_insert_seen'); PERFORM pg_sleep(2.5); RETURN NEW; END$$;
CREATE TRIGGER login205_delay BEFORE INSERT ON console_sessions FOR EACH ROW EXECUTE FUNCTION login205_delay()`); e != nil {
		t.Fatal(e)
	}
	// alive() judges the deadline by the database clock, so set it from that
	// clock; the test PostgreSQL VM clock drifts seconds from the host clock.
	var dbClock time.Time
	if e := p.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&dbClock); e != nil {
		t.Fatal(e)
	}
	g.until = dbClock.UTC().Add(2 * time.Second)
	_, _, issueError := s.IssueSession(t.Context(), g, u, sessions)
	if !errors.Is(issueError, ErrInvalid) {
		t.Fatal("last session write extended the original grant deadline", issueError)
	}
	var inserted bool
	var count int
	if e := p.QueryRow(t.Context(), `SELECT is_called,(SELECT count(*) FROM console_sessions) FROM login205_insert_seen`).Scan(&inserted, &count); e != nil || !inserted || count != 0 {
		t.Fatalf("expiry regression must reach INSERT and roll it back: inserted=%t sessions=%d query_error=%v issue_error=%v expired_by=%s", inserted, count, e, issueError, time.Since(g.until))
	}
}
func TestLoginCaptcha205SessionCommitHoldsSettingsLockIncludingPreviouslyAbsentFlag(t *testing.T) {
	s, p, sessions, u := sessionFixture205(t, 0)
	g := disabledGrant205(t, s)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	if _, e := p.Exec(ctx, `CREATE FUNCTION login205_pause() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM pg_advisory_xact_lock(205,117); RETURN NEW; END$$;CREATE TRIGGER login205_pause BEFORE INSERT ON console_sessions FOR EACH ROW EXECUTE FUNCTION login205_pause()`); e != nil {
		t.Fatal(e)
	}
	block, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(context.Background())
	if _, e = block.Exec(ctx, `SELECT pg_advisory_xact_lock(205,117)`); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, _, e := s.IssueSession(ctx, g, u, sessions); done <- e }()
	for {
		var waiting bool
		if e = p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=205 AND objid=117 AND NOT granted)`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case e := <-done:
			t.Fatal("session did not reach final insertion", e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	writer, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = writer.Exec(ctx, `SET LOCAL lock_timeout='100ms'`); e != nil {
		t.Fatal(e)
	}
	_, e = writer.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"captcha_open_login","value":"1"}')`)
	var lockError *pgconn.PgError
	if !errors.As(e, &lockError) || lockError.Code != "55P03" {
		t.Fatal("settings insertion did not wait on the session's held policy lock", e)
	}
	writer.Rollback(ctx)
	if e = block.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal("unchanged original policy failed after lock release", e)
	}
}
func TestLoginCaptcha205SessionSigningUsesOnePoolConnection(t *testing.T) {
	s, _, sessions, u := sessionFixture205(t, 1)
	g := disabledGrant205(t, s)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if token, csrf, e := s.IssueSession(ctx, g, u, sessions); e != nil || len(token) != 43 || len(csrf) != 43 {
		t.Fatal("session issuance needed a second connection or lost the original transaction", e)
	}
}
