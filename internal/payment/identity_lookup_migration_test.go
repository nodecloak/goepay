package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentityLookupMigrationPreservesHistoricalWebAndMiniClaims(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	nonce := make([]byte, 8)
	if _, e = rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	schema := "lookup_upgrade_" + hex.EncodeToString(nonce)
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	paths, e := filepath.Glob("../../db/migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range paths {
		if filepath.Base(path) >= "000047" {
			continue
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatal(path, e)
		}
	}
	s, e := NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	token := strings.Repeat("u", 43)
	lookupTestSession(t, s, token, "upgrade-user", 903)
	// Seed the historical order with the pre-47 schema. Current CreateOrder
	// intentionally writes newer snapshots and cannot run against this schema.
	o, e := scanOrder(pool.QueryRow(ctx, "INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount) VALUES(nextval('trade_no_seq')::text,903,'historical-oauth','test','alipay',1) RETURNING "+orderColumns))
	if e != nil {
		t.Fatal(e)
	}
	b := oauthTestBinding(t, s, o)
	browser := strings.Repeat("a", 64)
	state, e := s.IssueOAuthState(ctx, b, browser)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO oauth_code_claims(provider,app_id,code_hash,state_hash) VALUES('alipay','app',$1,$2)`, s.oauthHash("code", "historical-code"), s.oauthHash("state", state)); e != nil {
		t.Fatal(e)
	}
	mb := miniTestBinding(t, s, o)
	ms, e := s.IssueMiniState(ctx, mb)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO oauth_code_claims(provider,app_id,code_hash,mini_state_hash) VALUES('alipay','app',$1,$2)`, s.oauthHash("code", "historical-mini-code"), s.oauthHash("mini-state", ms)); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../db/migrations/000047_identity_lookup.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM oauth_authorization_code_claims WHERE domain='web' AND code_hash=$1 OR domain='mini' AND code_hash=$2`, s.oauthHash("code", "historical-code"), s.oauthHash("code", "historical-mini-code")).Scan(&n); e != nil || n != 2 {
		t.Fatal("historical domain backfill", n, e)
	}
	// Verify migration 47's original backfill before upgrading. Current APIs also
	// enforce the shared profile limit introduced by 64 and require current DDL.
	for _, path := range paths {
		if filepath.Base(path) < "000048" {
			continue
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatal(path, e)
		}
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM oauth_authorization_code_claims WHERE provider='alipay' AND app_id='app' AND domain='web' AND code_hash=$1`, s.oauthHash("code", "historical-mini-code")).Scan(&n); e != nil || n != 1 {
		t.Fatal("migration64 did not preserve historical mini realm", n, e)
	}
	q, e := s.CreateIdentityLookup(ctx, token, "merchant", IdentityLookupBinding{MerchantID: 903, ChannelID: 7, Provider: "alipay", AppID: "app", Scope: "auth_base", AccountScope: "synthetic-scope"})
	if e != nil {
		t.Fatal(e)
	}
	q, ls, e := s.StartIdentityLookup(ctx, q.AuthorizationToken, browser)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConsumeIdentityLookup(ctx, q.ID, "alipay", 7, ls, browser, "historical-code"); e == nil {
		t.Fatal("historical payment code reused after migration")
	}
	if _, e = s.ConsumeIdentityLookup(ctx, q.ID, "alipay", 7, ls, browser, "new-code"); e == nil {
		t.Fatal("historical code collision did not burn lookup state")
	}
}
