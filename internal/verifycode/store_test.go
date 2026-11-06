package verifycode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
)

func scoped(t *testing.T) *pgxpool.Pool {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(admin.Close)
	nonce := make([]byte, 8)
	rand.Read(nonce)
	schema := "vc134_" + hex.EncodeToString(nonce)
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, _ := pgxpool.ParseConfig(dsn)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close(); admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") })
	if e = db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	return p
}

func TestVerifyCode134IssueVerifyAndLimits(t *testing.T) {
	ctx := context.Background()
	p := scoped(t)
	s := New(p)

	// Issue a phone code; verifying the wrong value increments errcount, the
	// right value consumes it, and reuse then fails.
	code, e := s.Issue(ctx, Issue{Scene: "bind", Type: 1, Recipient: "13800000001", IP: "203.0.113.1"})
	if e != nil || len(code) != 6 {
		t.Fatal("issue phone code", code, e)
	}
	if e = s.Verify(ctx, "bind", 1, "13800000001", "000000", 0); !errors.Is(e, ErrWrong) {
		t.Fatal("wrong code should be ErrWrong", e)
	}
	if e = s.Verify(ctx, "bind", 1, "13800000001", code, 0); e != nil {
		t.Fatal("correct code should verify", e)
	}
	if e = s.Verify(ctx, "bind", 1, "13800000001", code, 0); !errors.Is(e, ErrExpired) {
		t.Fatal("consumed code must not verify again", e)
	}

	// 60-second gap: a second immediate send to the same recipient is refused.
	if _, e = s.Issue(ctx, Issue{Scene: "reg", Type: 1, Recipient: "13800000002", IP: "203.0.113.2"}); e != nil {
		t.Fatal("first send", e)
	}
	if _, e = s.Issue(ctx, Issue{Scene: "reg", Type: 1, Recipient: "13800000002", IP: "203.0.113.2"}); !errors.Is(e, ErrTooSoon) {
		t.Fatal("second send within 60s must be refused", e)
	}

	// No verifiable code for an unknown recipient.
	if e = s.Verify(ctx, "reg", 1, "13800009999", "123456", 0); !errors.Is(e, ErrNoCode) {
		t.Fatal("unknown recipient should be ErrNoCode", e)
	}

	// 5 wrong attempts invalidate even the correct code.
	c3, e := s.Issue(ctx, Issue{Scene: "find", Type: 1, Recipient: "13800000003", IP: "203.0.113.3"})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 5; i++ {
		if e = s.Verify(ctx, "find", 1, "13800000003", "000000", 0); !errors.Is(e, ErrWrong) {
			t.Fatal("wrong attempt", i, e)
		}
	}
	if e = s.Verify(ctx, "find", 1, "13800000003", c3, 0); !errors.Is(e, ErrExpired) {
		t.Fatal("after 5 wrong attempts the correct code must be refused", e)
	}

	// Per-phone daily cap (3): backdate past the 60s gap but inside 24h, so the
	// 4th send trips the recipient cap rather than the gap.
	phone := "13800000004"
	for i := 0; i < 3; i++ {
		if _, e = p.Exec(ctx, `INSERT INTO verification_codes134(scene,type,recipient,code,ip,created_at)VALUES('reg',1,$1,'654321','203.0.113.4',clock_timestamp()-interval '10 minutes')`, phone); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.Issue(ctx, Issue{Scene: "reg", Type: 1, Recipient: phone, IP: "203.0.113.9"}); !errors.Is(e, ErrRecipLim) {
		t.Fatal("4th send to a phone in 24h must trip the recipient cap", e)
	}

	// Per-IP daily cap (6 for SMS): seed 6 distinct recipients from one IP in 24h.
	ip := "203.0.113.50"
	for i := 0; i < 6; i++ {
		if _, e = p.Exec(ctx, `INSERT INTO verification_codes134(scene,type,recipient,code,ip,created_at)VALUES('reg',1,$1,'654321',$2,clock_timestamp()-interval '10 minutes')`, "1390000000"+string(rune('0'+i)), ip); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.Issue(ctx, Issue{Scene: "reg", Type: 1, Recipient: "13900000099", IP: ip}); !errors.Is(e, ErrIPLim) {
		t.Fatal("7th send from one IP in 24h must trip the IP cap", e)
	}

	// Email code is 7 digits and uses the email caps.
	ec, e := s.Issue(ctx, Issue{Scene: "find", Type: 0, Recipient: "u@example.invalid", IP: "203.0.113.70"})
	if e != nil || len(ec) != 7 {
		t.Fatal("email code should be 7 digits", ec, e)
	}

	// IssueWithSender persists ONLY on a successful send; a failed send stores
	// nothing and does not count toward the caps (original inserts after send).
	recip := "13800007777"
	sendErr := errors.New("provider down")
	if e = s.IssueWithSender(ctx, Issue{Scene: "reg", Type: 1, Recipient: recip, IP: "203.0.113.80"}, func(string) error { return sendErr }); !errors.Is(e, sendErr) {
		t.Fatal("failed send should surface its error", e)
	}
	var rows int
	if e = p.QueryRow(ctx, `SELECT count(*)FROM verification_codes134 WHERE recipient=$1`, recip).Scan(&rows); e != nil || rows != 0 {
		t.Fatal("failed send must persist no code", rows, e)
	}
	var sent string
	if e = s.IssueWithSender(ctx, Issue{Scene: "reg", Type: 1, Recipient: recip, IP: "203.0.113.80"}, func(c string) error { sent = c; return nil }); e != nil {
		t.Fatal("successful send should persist", e)
	}
	if len(sent) != 6 {
		t.Fatal("sender should receive the 6-digit phone code", sent)
	}
	if e = s.Verify(ctx, "reg", 1, recip, sent, 0); e != nil {
		t.Fatal("persisted code should verify", e)
	}
}
