package connectstate

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
	n := make([]byte, 8)
	rand.Read(n)
	schema := "cls135_" + hex.EncodeToString(n)
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") })
	cfg, _ := pgxpool.ParseConfig(dsn)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	if e = db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	return p
}

func TestConnectState135IssueConsumeSingleUseAndBound(t *testing.T) {
	ctx := context.Background()
	p := scoped(t)
	if _, e := p.Exec(ctx, `INSERT INTO merchants(id,name,api_key)VALUES(7001,'m','synthetic-key')`); e != nil {
		t.Fatal(e)
	}
	s := New(p)

	// login state (no merchant): consume once with the right browser nonce.
	iss, e := s.Issue(ctx, "qq", "login", 0)
	if e != nil || iss.State == "" || iss.Browser == "" {
		t.Fatal("issue login", iss, e)
	}
	// wrong browser nonce is refused and does NOT consume.
	if _, e = s.Consume(ctx, "qq", iss.State, "wrong-browser-nonce"); !errors.Is(e, ErrState) {
		t.Fatal("wrong browser should be ErrState", e)
	}
	// wrong provider is refused.
	if _, e = s.Consume(ctx, "wx", iss.State, iss.Browser); !errors.Is(e, ErrState) {
		t.Fatal("wrong provider should be ErrState", e)
	}
	claim, e := s.Consume(ctx, "qq", iss.State, iss.Browser)
	if e != nil || claim.Provider != "qq" || claim.Purpose != "login" || claim.Merchant != 0 {
		t.Fatal("login consume", claim, e)
	}
	// single-use: a second consume fails.
	if _, e = s.Consume(ctx, "qq", iss.State, iss.Browser); !errors.Is(e, ErrState) {
		t.Fatal("second consume should be ErrState", e)
	}

	// bind state carries the merchant.
	bi, e := s.Issue(ctx, "qq", "bind", 7001)
	if e != nil {
		t.Fatal("issue bind", e)
	}
	bc, e := s.Consume(ctx, "qq", bi.State, bi.Browser)
	if e != nil || bc.Purpose != "bind" || bc.Merchant != 7001 {
		t.Fatal("bind consume should carry merchant", bc, e)
	}

	// purpose/merchant mismatch is refused at issue.
	if _, e = s.Issue(ctx, "qq", "bind", 0); !errors.Is(e, ErrSource) {
		t.Fatal("bind without merchant should be refused", e)
	}
	if _, e = s.Issue(ctx, "qq", "login", 7001); !errors.Is(e, ErrSource) {
		t.Fatal("login with merchant should be refused", e)
	}

	// an unknown state is refused.
	if _, e = s.Consume(ctx, "qq", "deadbeef", "x"); !errors.Is(e, ErrState) {
		t.Fatal("unknown state should be ErrState", e)
	}

	// pending-connect: issue a resolved-but-unbound identity, consume once.
	pstate, e := s.IssuePending(ctx, "qq", "QQ-UNBOUND", "pbrowser")
	if e != nil || pstate == "" {
		t.Fatal("issue pending", pstate, e)
	}
	if _, e = s.ConsumePending(ctx, pstate, "wrong"); !errors.Is(e, ErrState) {
		t.Fatal("wrong browser pending should be ErrState", e)
	}
	pc, e := s.ConsumePending(ctx, pstate, "pbrowser")
	if e != nil || pc.Provider != "qq" || pc.SocialUID != "QQ-UNBOUND" {
		t.Fatal("pending consume", pc, e)
	}
	if _, e = s.ConsumePending(ctx, pstate, "pbrowser"); !errors.Is(e, ErrState) {
		t.Fatal("pending is single-use", e)
	}
}
